package authentication

import (
	"strings"
	"sync"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type attemptBucket struct {
	failures  int
	pending   int
	startedAt time.Time
}

// AttemptLimiter limits failed authentication attempts by username and source IP.
type AttemptLimiter struct {
	mutex sync.Mutex

	usernameLimit int
	ipLimit       int
	window        time.Duration

	usernames   map[string]attemptBucket
	ipAddresses map[string]attemptBucket
	nextCleanup time.Time
}

// NewAttemptLimiter creates an in-memory authentication attempt limiter.
func NewAttemptLimiter(
	usernameLimit int,
	ipLimit int,
	window time.Duration,
) *AttemptLimiter {
	return &AttemptLimiter{
		usernameLimit: usernameLimit,
		ipLimit:       ipLimit,
		window:        window,
		usernames:     make(map[string]attemptBucket),
		ipAddresses:   make(map[string]attemptBucket),
	}
}

// Allow reports whether both independent failure buckets permit an attempt.
func (limiter *AttemptLimiter) Allow(
	username string,
	sourceIP string,
	now time.Time,
) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(now)

	usernameKey := limiterUsernameKey(username)

	usernameBucket := limiter.usernames[usernameKey]
	ipBucket := limiter.ipAddresses[sourceIP]

	if !bucketAllows(
		usernameBucket,
		limiter.usernameLimit,
		now,
		limiter.window,
	) || !bucketAllows(
		ipBucket,
		limiter.ipLimit,
		now,
		limiter.window,
	) {
		return false
	}

	limiter.usernames[usernameKey] = reserveBucket(
		usernameBucket,
		now,
		limiter.window,
	)
	limiter.ipAddresses[sourceIP] = reserveBucket(
		ipBucket,
		now,
		limiter.window,
	)

	return true
}

// RecordFailure increments both independent failure buckets.
func (limiter *AttemptLimiter) RecordFailure(
	username string,
	sourceIP string,
	now time.Time,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(now)

	usernameKey := limiterUsernameKey(username)

	limiter.usernames[usernameKey] = completeFailedAttempt(
		limiter.usernames[usernameKey],
		now,
		limiter.window,
	)
	limiter.ipAddresses[sourceIP] = completeFailedAttempt(
		limiter.ipAddresses[sourceIP],
		now,
		limiter.window,
	)
}

// RecordSuccess clears username failures and releases the IP reservation.
func (limiter *AttemptLimiter) RecordSuccess(
	username string,
	sourceIP string,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	delete(
		limiter.usernames,
		limiterUsernameKey(username),
	)
	releaseReservation(limiter.ipAddresses, sourceIP)
}

// Cancel releases a reserved attempt without recording a failure.
func (limiter *AttemptLimiter) Cancel(
	username string,
	sourceIP string,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	releaseReservation(
		limiter.usernames,
		limiterUsernameKey(username),
	)
	releaseReservation(
		limiter.ipAddresses,
		sourceIP,
	)
}

func releaseReservation(
	buckets map[string]attemptBucket,
	key string,
) {
	bucket, exists := buckets[key]
	if !exists {
		return
	}

	if bucket.pending > 0 {
		bucket.pending--
	}

	if bucket.failures == 0 && bucket.pending == 0 {
		delete(buckets, key)

		return
	}

	buckets[key] = bucket
}

func limiterUsernameKey(username string) string {
	normalizedUsername, err := identity.NormalizeUsername(username)
	if err == nil {
		return normalizedUsername
	}

	return strings.ToLower(strings.TrimSpace(username))
}

func bucketAllows(
	bucket attemptBucket,
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

func reserveBucket(
	bucket attemptBucket,
	now time.Time,
	window time.Duration,
) attemptBucket {
	if bucket.startedAt.IsZero() ||
		!now.Before(bucket.startedAt.Add(window)) {
		return attemptBucket{
			pending:   1,
			startedAt: now,
		}
	}

	bucket.pending++

	return bucket
}

func completeFailedAttempt(
	bucket attemptBucket,
	now time.Time,
	window time.Duration,
) attemptBucket {
	if bucket.startedAt.IsZero() ||
		!now.Before(bucket.startedAt.Add(window)) {
		return attemptBucket{
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

func (limiter *AttemptLimiter) cleanupExpired(now time.Time) {
	if !limiter.nextCleanup.IsZero() &&
		now.Before(limiter.nextCleanup) {
		return
	}

	for key, bucket := range limiter.usernames {
		if !now.Before(bucket.startedAt.Add(limiter.window)) {
			delete(limiter.usernames, key)
		}
	}

	for key, bucket := range limiter.ipAddresses {
		if !now.Before(bucket.startedAt.Add(limiter.window)) {
			delete(limiter.ipAddresses, key)
		}
	}

	limiter.nextCleanup = now.Add(limiter.window)
}
