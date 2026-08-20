package authentication

import (
	"fmt"
	"sync"
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

	limiter.RecordSuccess("archive_admin")

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

	if len(limiter.usernames) != 0 {
		t.Fatalf(
			"expected expired username buckets to be removed, got %d",
			len(limiter.usernames),
		)
	}
	if len(limiter.ipAddresses) != 0 {
		t.Fatalf(
			"expected expired IP buckets to be removed, got %d",
			len(limiter.ipAddresses),
		)
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
