package passwords

import (
	"testing"
	"time"
)

func TestIPAttemptLimiterLimitsEachSourceIndependently(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewIPAttemptLimiter(2, time.Minute)

	for range 2 {
		if !limiter.Allow("192.0.2.10", now) {
			t.Fatal("expected source IP attempt to be allowed")
		}
		limiter.RecordFailure("192.0.2.10", now)
	}

	if limiter.Allow("192.0.2.10", now) {
		t.Fatal("expected source IP limit to be enforced")
	}
	if !limiter.Allow("192.0.2.11", now) {
		t.Fatal("expected an independent source IP bucket")
	}
}

func TestIPAttemptLimiterReleasesCanceledAndSuccessfulAttempts(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	limiter := NewIPAttemptLimiter(1, time.Minute)

	if !limiter.Allow("192.0.2.10", now) {
		t.Fatal("expected initial attempt to be allowed")
	}
	limiter.Cancel("192.0.2.10")
	if !limiter.Allow("192.0.2.10", now) {
		t.Fatal("expected canceled reservation to be released")
	}
	limiter.RecordSuccess("192.0.2.10")
	if !limiter.Allow("192.0.2.10", now) {
		t.Fatal("expected success to clear the source IP bucket")
	}
}

func TestIPAttemptLimiterExpiresFailures(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	window := time.Minute
	limiter := NewIPAttemptLimiter(1, window)

	if !limiter.Allow("192.0.2.10", now) {
		t.Fatal("expected initial attempt to be allowed")
	}
	limiter.RecordFailure("192.0.2.10", now)

	if !limiter.Allow("192.0.2.10", now.Add(window)) {
		t.Fatal("expected expired failure to be removed")
	}
}
