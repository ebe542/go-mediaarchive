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
	argLimit int,
	argWindow time.Duration,
) *IPAttemptLimiter {
	return &IPAttemptLimiter{
		limit:   argLimit,
		window:  argWindow,
		buckets: make(map[string]ipAttemptBucket),
	}
}

// Allow reserves capacity for an enrollment attempt when its IP is permitted.
func (limiter *IPAttemptLimiter) Allow(
	argSourceIP string,
	argNow time.Time,
) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(argNow)

	bucket := limiter.buckets[argSourceIP]
	if !ipBucketAllows(bucket, limiter.limit, argNow, limiter.window) {
		return false
	}

	limiter.buckets[argSourceIP] = reserveIPBucket(
		bucket,
		argNow,
		limiter.window,
	)

	return true
}

// RecordFailure converts a reserved attempt into a failed attempt.
func (limiter *IPAttemptLimiter) RecordFailure(
	argSourceIP string,
	argNow time.Time,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(argNow)
	limiter.buckets[argSourceIP] = completeFailedIPAttempt(
		limiter.buckets[argSourceIP],
		argNow,
		limiter.window,
	)
}

// RecordSuccess clears previous failures for the source IP.
func (limiter *IPAttemptLimiter) RecordSuccess(argSourceIP string) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	delete(limiter.buckets, argSourceIP)
}

// Cancel releases capacity after an attempt that must not affect the limit.
func (limiter *IPAttemptLimiter) Cancel(argSourceIP string) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	bucket, exists := limiter.buckets[argSourceIP]
	if !exists {
		return
	}

	if bucket.pending > 0 {
		bucket.pending--
	}

	if bucket.failures == 0 && bucket.pending == 0 {
		delete(limiter.buckets, argSourceIP)

		return
	}

	limiter.buckets[argSourceIP] = bucket
}

func ipBucketAllows(
	argBucket ipAttemptBucket,
	argLimit int,
	argNow time.Time,
	argWindow time.Duration,
) bool {
	if argBucket.startedAt.IsZero() ||
		!argNow.Before(argBucket.startedAt.Add(argWindow)) {
		return true
	}

	return argBucket.failures+argBucket.pending < argLimit
}

func reserveIPBucket(
	argBucket ipAttemptBucket,
	argNow time.Time,
	argWindow time.Duration,
) ipAttemptBucket {
	if argBucket.startedAt.IsZero() ||
		!argNow.Before(argBucket.startedAt.Add(argWindow)) {
		return ipAttemptBucket{
			pending:   1,
			startedAt: argNow,
		}
	}

	argBucket.pending++

	return argBucket
}

func completeFailedIPAttempt(
	argBucket ipAttemptBucket,
	argNow time.Time,
	argWindow time.Duration,
) ipAttemptBucket {
	if argBucket.startedAt.IsZero() ||
		!argNow.Before(argBucket.startedAt.Add(argWindow)) {
		return ipAttemptBucket{
			failures:  1,
			startedAt: argNow,
		}
	}

	if argBucket.pending > 0 {
		argBucket.pending--
	}
	argBucket.failures++

	return argBucket
}

func (limiter *IPAttemptLimiter) cleanupExpired(argNow time.Time) {
	if !limiter.nextCleanup.IsZero() && argNow.Before(limiter.nextCleanup) {
		return
	}

	for key, bucket := range limiter.buckets {
		if !argNow.Before(bucket.startedAt.Add(limiter.window)) {
			delete(limiter.buckets, key)
		}
	}

	limiter.nextCleanup = argNow.Add(limiter.window)
}
