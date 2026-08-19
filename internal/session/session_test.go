package session

import (
	"crypto/sha256"
	"testing"
	"time"
)

func TestNewCreatesActiveSessionWithAbsoluteExpiration(t *testing.T) {
	now := time.Date(
		2026,
		time.August,
		19,
		10,
		0,
		0,
		0,
		time.FixedZone("test", 2*60*60),
	)
	tokenHash := sha256.Sum256([]byte("synthetic-session-token"))
	userID := "123e4567-e89b-12d3-a456-426614174000"
	lifetime := 8 * time.Hour

	createdSession, err := New(
		tokenHash,
		userID,
		now,
		lifetime,
	)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	expectedTime := now.UTC()

	if createdSession.TokenHash != tokenHash {
		t.Fatal("expected token hash to be preserved")
	}
	if createdSession.UserID != userID {
		t.Fatalf(
			"expected user ID %q, got %q",
			userID,
			createdSession.UserID,
		)
	}
	if createdSession.CreatedAt != expectedTime {
		t.Fatalf(
			"expected creation time %v, got %v",
			expectedTime,
			createdSession.CreatedAt,
		)
	}
	if createdSession.LastSeenAt != expectedTime {
		t.Fatalf(
			"expected last-seen time %v, got %v",
			expectedTime,
			createdSession.LastSeenAt,
		)
	}
	if createdSession.ExpiresAt != expectedTime.Add(lifetime) {
		t.Fatalf(
			"expected expiration time %v, got %v",
			expectedTime.Add(lifetime),
			createdSession.ExpiresAt,
		)
	}
	if !createdSession.RevokedAt.IsZero() {
		t.Fatalf(
			"expected no revocation time, got %v",
			createdSession.RevokedAt,
		)
	}
}

func TestSessionIsValidAt(t *testing.T) {
	baseTime := time.Date(
		2026,
		time.August,
		19,
		10,
		0,
		0,
		0,
		time.UTC,
	)
	tokenHash := sha256.Sum256([]byte("synthetic-session-token"))

	activeSession, err := New(
		tokenHash,
		"123e4567-e89b-12d3-a456-426614174000",
		baseTime,
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create session fixture: %v", err)
	}

	revokedSession := activeSession
	revokedSession.RevokedAt = baseTime.Add(time.Minute)

	testCases := map[string]struct {
		session    Session
		now        time.Time
		userActive bool
		expected   bool
	}{
		"active session": {
			session:    activeSession,
			now:        baseTime.Add(29 * time.Minute),
			userActive: true,
			expected:   true,
		},
		"idle timeout reached": {
			session:    activeSession,
			now:        baseTime.Add(30 * time.Minute),
			userActive: true,
		},
		"absolute expiration reached": {
			session:    activeSession,
			now:        baseTime.Add(8 * time.Hour),
			userActive: true,
		},
		"revoked session": {
			session:    revokedSession,
			now:        baseTime.Add(2 * time.Minute),
			userActive: true,
		},
		"inactive user": {
			session: activeSession,
			now:     baseTime.Add(time.Minute),
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			actual := testCase.session.IsValidAt(
				testCase.now,
				30*time.Minute,
				testCase.userActive,
			)

			if actual != testCase.expected {
				t.Fatalf(
					"expected validity %t, got %t",
					testCase.expected,
					actual,
				)
			}
		})
	}
}
