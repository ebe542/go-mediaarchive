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
	argUsernameLimit int,
	argIPLimit int,
	argWindow time.Duration,
) *AttemptLimiter {
	return &AttemptLimiter{
		usernameLimit: argUsernameLimit,
		ipLimit:       argIPLimit,
		window:        argWindow,
		usernames:     make(map[string]attemptBucket),
		ipAddresses:   make(map[string]attemptBucket),
	}
}

// Allow reports whether both independent failure buckets permit an attempt.
func (limiter *AttemptLimiter) Allow(
	argUsername string,
	argSourceIP string,
	argNow time.Time,
) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(argNow)

	username := limiterUsernameKey(argUsername)

	usernameBucket := limiter.usernames[username]
	ipBucket := limiter.ipAddresses[argSourceIP]

	if !bucketAllows(
		usernameBucket,
		limiter.usernameLimit,
		argNow,
		limiter.window,
	) || !bucketAllows(
		ipBucket,
		limiter.ipLimit,
		argNow,
		limiter.window,
	) {
		return false
	}

	limiter.usernames[username] = reserveBucket(
		usernameBucket,
		argNow,
		limiter.window,
	)
	limiter.ipAddresses[argSourceIP] = reserveBucket(
		ipBucket,
		argNow,
		limiter.window,
	)

	return true
}

// RecordFailure increments both independent failure buckets.
func (limiter *AttemptLimiter) RecordFailure(
	argUsername string,
	argSourceIP string,
	argNow time.Time,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	limiter.cleanupExpired(argNow)

	username := limiterUsernameKey(argUsername)

	limiter.usernames[username] = completeFailedAttempt(
		limiter.usernames[username],
		argNow,
		limiter.window,
	)
	limiter.ipAddresses[argSourceIP] = completeFailedAttempt(
		limiter.ipAddresses[argSourceIP],
		argNow,
		limiter.window,
	)
}

// RecordSuccess clears username failures and releases the IP reservation.
func (limiter *AttemptLimiter) RecordSuccess(
	argUsername string,
	argSourceIP string,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	delete(
		limiter.usernames,
		limiterUsernameKey(argUsername),
	)
	releaseReservation(limiter.ipAddresses, argSourceIP)
}

// Cancel releases a reserved attempt without recording a failure.
func (limiter *AttemptLimiter) Cancel(
	argUsername string,
	argSourceIP string,
) {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	releaseReservation(
		limiter.usernames,
		limiterUsernameKey(argUsername),
	)
	releaseReservation(
		limiter.ipAddresses,
		argSourceIP,
	)
}

func releaseReservation(
	argBuckets map[string]attemptBucket,
	argKey string,
) {
	bucket, exists := argBuckets[argKey]
	if !exists {
		return
	}

	if bucket.pending > 0 {
		bucket.pending--
	}

	if bucket.failures == 0 && bucket.pending == 0 {
		delete(argBuckets, argKey)

		return
	}

	argBuckets[argKey] = bucket
}

func limiterUsernameKey(argUsername string) string {
	normalizedUsername, err := identity.NormalizeUsername(argUsername)
	if err == nil {
		return normalizedUsername
	}

	return strings.ToLower(strings.TrimSpace(argUsername))
}

func bucketAllows(
	argBucket attemptBucket,
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

func reserveBucket(
	argBucket attemptBucket,
	argNow time.Time,
	argWindow time.Duration,
) attemptBucket {
	if argBucket.startedAt.IsZero() ||
		!argNow.Before(argBucket.startedAt.Add(argWindow)) {
		return attemptBucket{
			pending:   1,
			startedAt: argNow,
		}
	}

	argBucket.pending++

	return argBucket
}

func completeFailedAttempt(
	argBucket attemptBucket,
	argNow time.Time,
	argWindow time.Duration,
) attemptBucket {
	if argBucket.startedAt.IsZero() ||
		!argNow.Before(argBucket.startedAt.Add(argWindow)) {
		return attemptBucket{
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

func (limiter *AttemptLimiter) cleanupExpired(argNow time.Time) {
	if !limiter.nextCleanup.IsZero() &&
		argNow.Before(limiter.nextCleanup) {
		return
	}

	for key, bucket := range limiter.usernames {
		if !argNow.Before(bucket.startedAt.Add(limiter.window)) {
			delete(limiter.usernames, key)
		}
	}

	for key, bucket := range limiter.ipAddresses {
		if !argNow.Before(bucket.startedAt.Add(limiter.window)) {
			delete(limiter.ipAddresses, key)
		}
	}

	limiter.nextCleanup = argNow.Add(limiter.window)
}
