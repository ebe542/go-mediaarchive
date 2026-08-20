package authentication

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAttemptLimiterAppliesIndependentUsernameAndIPLimits(
	t *testing.T,
) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	limiter := NewAttemptLimiter(
		5,
		20,
		15*time.Minute,
	)

	for attempt := 0; attempt < 5; attempt++ {
		limiter.RecordFailure(
			"target_user",
			fmt.Sprintf("192.0.2.%d", attempt+1),
			now,
		)
	}

	if limiter.Allow("target_user", "198.51.100.1", now) {
		t.Fatal("expected username limit to reject another source IP")
	}

	if !limiter.Allow("different_user", "198.51.100.1", now) {
		t.Fatal("expected unrelated username and IP to remain allowed")
	}

	for attempt := 0; attempt < 20; attempt++ {
		limiter.RecordFailure(
			fmt.Sprintf("spray_user_%d", attempt),
			"203.0.113.10",
			now,
		)
	}

	if limiter.Allow("new_spray_user", "203.0.113.10", now) {
		t.Fatal("expected source IP limit to reject another username")
	}

	if !limiter.Allow("new_spray_user", "203.0.113.11", now) {
		t.Fatal("expected unrelated source IP to remain allowed")
	}
}

func TestAttemptLimiterExpiresAndClearsUsernameFailures(
	t *testing.T,
) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	window := 15 * time.Minute
	limiter := NewAttemptLimiter(1, 20, window)

	limiter.RecordFailure(
		"archive_admin",
		"192.0.2.1",
		now,
	)

	if limiter.Allow("archive_admin", "192.0.2.2", now) {
		t.Fatal("expected username to be limited")
	}

	limiter.RecordSuccess("archive_admin", "192.0.2.2")

	if !limiter.Allow("archive_admin", "192.0.2.2", now) {
		t.Fatal("expected successful authentication to clear username failures")
	}

	limiter.RecordFailure(
		"archive_admin",
		"192.0.2.1",
		now,
	)

	if !limiter.Allow(
		"archive_admin",
		"192.0.2.1",
		now.Add(window),
	) {
		t.Fatal("expected failures to expire at the window boundary")
	}
}
func TestAttemptLimiterNormalizesUsernameAndRemovesExpiredBuckets(
	t *testing.T,
) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	window := 15 * time.Minute
	limiter := NewAttemptLimiter(1, 20, window)

	limiter.RecordFailure(
		" Archive_Admin ",
		"192.0.2.1",
		now,
	)

	if limiter.Allow("archive_admin", "192.0.2.2", now) {
		t.Fatal("expected normalized username to share one bucket")
	}

	if !limiter.Allow(
		"unrelated_user",
		"198.51.100.1",
		now.Add(window),
	) {
		t.Fatal("expected attempt after window expiration to be allowed")
	}

	if _, exists := limiter.usernames["archive_admin"]; exists {
		t.Fatal("expected expired username bucket to be removed")
	}

	if _, exists := limiter.ipAddresses["192.0.2.1"]; exists {
		t.Fatal("expected expired IP bucket to be removed")
	}

	if _, exists := limiter.usernames["unrelated_user"]; !exists {
		t.Fatal("expected allowed username attempt to be reserved")
	}

	if _, exists := limiter.ipAddresses["198.51.100.1"]; !exists {
		t.Fatal("expected allowed IP attempt to be reserved")
	}
}

func TestAttemptLimiterRecordsConcurrentFailuresSafely(t *testing.T) {
	const failureCount = 100

	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	limiter := NewAttemptLimiter(
		failureCount+1,
		failureCount+1,
		15*time.Minute,
	)

	var waitGroup sync.WaitGroup
	waitGroup.Add(failureCount)

	for attempt := 0; attempt < failureCount; attempt++ {
		go func() {
			defer waitGroup.Done()

			limiter.RecordFailure(
				"archive_admin",
				"192.0.2.1",
				now,
			)
		}()
	}

	waitGroup.Wait()

	usernameBucket := limiter.usernames["archive_admin"]
	if usernameBucket.failures != failureCount {
		t.Fatalf(
			"expected %d username failures, got %d",
			failureCount,
			usernameBucket.failures,
		)
	}

	ipBucket := limiter.ipAddresses["192.0.2.1"]
	if ipBucket.failures != failureCount {
		t.Fatalf(
			"expected %d IP failures, got %d",
			failureCount,
			ipBucket.failures,
		)
	}
}

func TestAttemptLimiterReservesConcurrentAttempts(t *testing.T) {
	const attemptCount = 100

	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	limiter := NewAttemptLimiter(
		5,
		attemptCount,
		15*time.Minute,
	)

	var allowedCount atomic.Int64
	var waitGroup sync.WaitGroup
	waitGroup.Add(attemptCount)

	for attempt := 0; attempt < attemptCount; attempt++ {
		go func(argAttempt int) {
			defer waitGroup.Done()

			if limiter.Allow(
				"archive_admin",
				fmt.Sprintf("192.0.2.%d", argAttempt+1),
				now,
			) {
				allowedCount.Add(1)
			}
		}(attempt)
	}

	waitGroup.Wait()

	if actual := allowedCount.Load(); actual != 5 {
		t.Fatalf(
			"expected exactly %d reserved attempts, got %d",
			5,
			actual,
		)
	}
}

func TestAttemptLimiterCancelsReservedAttempt(t *testing.T) {
	now := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	limiter := NewAttemptLimiter(
		1,
		1,
		15*time.Minute,
	)

	if !limiter.Allow("archive_admin", "192.0.2.1", now) {
		t.Fatal("expected first attempt to be allowed")
	}

	limiter.Cancel("archive_admin", "192.0.2.1")

	if !limiter.Allow("archive_admin", "192.0.2.1", now) {
		t.Fatal("expected canceled reservation to release both limits")
	}
}
