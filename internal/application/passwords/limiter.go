package passwords

import (
	"sync"
	"time"
)

type ipAttemptBucket struct {
	failures  int
	pending   int
	startedAt time.Time
}

// IPAttemptLimiter limits failed enrollment attempts solely by source IP.
type IPAttemptLimiter struct {
	mutex sync.Mutex

	limit  int
	window time.Duration

	buckets     map[string]ipAttemptBucket
	nextCleanup time.Time
}

// NewIPAttemptLimiter creates an in-memory source-IP attempt limiter.
func NewIPAttemptLimiter(
	limit int,
	window time.Duration,
) *IPAttemptLimiter {
	return &IPAttemptLimiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string]ipAttemptBucket),
	}
}

// Allow reserves capacity for an enrollment attempt when its IP is permitted.
func (limiter *IPAttemptLimiter) Allow(
	sourceIP string,
	now time.Time,
) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(now)

	bucket := limiter.buckets[sourceIP]
	if !ipBucketAllows(bucket, limiter.limit, now, limiter.window) {
		return false
	}

	limiter.buckets[sourceIP] = reserveIPBucket(
		bucket,
		now,
		limiter.window,
	)

	return true
}

// RecordFailure converts a reserved attempt into a failed attempt.
func (limiter *IPAttemptLimiter) RecordFailure(
	sourceIP string,
	now time.Time,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(now)
	limiter.buckets[sourceIP] = completeFailedIPAttempt(
		limiter.buckets[sourceIP],
		now,
		limiter.window,
	)
}

// RecordSuccess clears previous failures for the source IP.
func (limiter *IPAttemptLimiter) RecordSuccess(sourceIP string) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	delete(limiter.buckets, sourceIP)
}

// Cancel releases capacity after an attempt that must not affect the limit.
func (limiter *IPAttemptLimiter) Cancel(sourceIP string) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	bucket, exists := limiter.buckets[sourceIP]
	if !exists {
		return
	}

	if bucket.pending > 0 {
		bucket.pending--
	}

	if bucket.failures == 0 && bucket.pending == 0 {
		delete(limiter.buckets, sourceIP)

		return
	}

	limiter.buckets[sourceIP] = bucket
}

func ipBucketAllows(
	bucket ipAttemptBucket,
	limit int,
	now time.Time,
	window time.Duration,
) bool {
	if bucket.startedAt.IsZero() ||
		!now.Before(bucket.startedAt.Add(window)) {
		return true
	}

	return bucket.failures+bucket.pending < limit
}

func reserveIPBucket(
	bucket ipAttemptBucket,
	now time.Time,
	window time.Duration,
) ipAttemptBucket {
	if bucket.startedAt.IsZero() ||
		!now.Before(bucket.startedAt.Add(window)) {
		return ipAttemptBucket{
			pending:   1,
			startedAt: now,
		}
	}

	bucket.pending++

	return bucket
}

func completeFailedIPAttempt(
	bucket ipAttemptBucket,
	now time.Time,
	window time.Duration,
) ipAttemptBucket {
	if bucket.startedAt.IsZero() ||
		!now.Before(bucket.startedAt.Add(window)) {
		return ipAttemptBucket{
			failures:  1,
			startedAt: now,
		}
	}

	if bucket.pending > 0 {
		bucket.pending--
	}
	bucket.failures++

	return bucket
}

func (limiter *IPAttemptLimiter) cleanupExpired(now time.Time) {
	if !limiter.nextCleanup.IsZero() && now.Before(limiter.nextCleanup) {
		return
	}

	for key, bucket := range limiter.buckets {
		if !now.Before(bucket.startedAt.Add(limiter.window)) {
			delete(limiter.buckets, key)
		}
	}

	limiter.nextCleanup = now.Add(limiter.window)
}
