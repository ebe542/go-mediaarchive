package credential

import (
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

func TestNewPasswordEnrollment(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.FixedZone("test", 3600))
	tokenHash := sha256.Sum256([]byte("enrollment-token"))

	enrollment, err := NewPasswordEnrollment(
		"123e4567-e89b-12d3-a456-426614174000",
		tokenHash,
		now,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create password enrollment: %v", err)
	}

	if enrollment.CreatedAt.Location() != time.UTC {
		t.Fatal("expected UTC creation time")
	}
	if !enrollment.ExpiresAt.Equal(enrollment.CreatedAt.Add(24 * time.Hour)) {
		t.Fatalf("unexpected expiration time %s", enrollment.ExpiresAt)
	}
}

func TestNewPasswordEnrollmentRejectsInvalidValues(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	validUserID := "123e4567-e89b-12d3-a456-426614174000"
	validHash := sha256.Sum256([]byte("enrollment-token"))

	testCases := []struct {
		name        string
		userID      string
		tokenHash   [sha256.Size]byte
		now         time.Time
		lifetime    time.Duration
		expectedErr error
	}{
		{"invalid user ID", "not-a-uuid", validHash, now, time.Hour, ErrInvalidEnrollmentUserID},
		{"empty token hash", validUserID, [sha256.Size]byte{}, now, time.Hour, ErrInvalidEnrollmentTokenHash},
		{"zero time", validUserID, validHash, time.Time{}, time.Hour, ErrInvalidEnrollmentTimestamp},
		{"zero lifetime", validUserID, validHash, now, 0, ErrInvalidEnrollmentLifetime},
		{"negative lifetime", validUserID, validHash, now, -time.Hour, ErrInvalidEnrollmentLifetime},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := NewPasswordEnrollment(
				testCase.userID,
				testCase.tokenHash,
				testCase.now,
				testCase.lifetime,
			)
			if !errors.Is(err, testCase.expectedErr) {
				t.Fatalf("expected %v, got %v", testCase.expectedErr, err)
			}
		})
	}
}

func TestPasswordEnrollmentValidity(t *testing.T) {
	createdAt := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	enrollment, err := NewPasswordEnrollment(
		"123e4567-e89b-12d3-a456-426614174000",
		sha256.Sum256([]byte("enrollment-token")),
		createdAt,
		time.Hour,
	)
	if err != nil {
		t.Fatalf("create password enrollment: %v", err)
	}

	testCases := []struct {
		name     string
		now      time.Time
		expected bool
	}{
		{"zero time", time.Time{}, false},
		{"before creation", createdAt.Add(-time.Nanosecond), false},
		{"at creation", createdAt, true},
		{"before expiration", createdAt.Add(time.Hour - time.Nanosecond), true},
		{"at expiration", createdAt.Add(time.Hour), false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if actual := enrollment.IsValidAt(testCase.now); actual != testCase.expected {
				t.Fatalf("expected %t, got %t", testCase.expected, actual)
			}
		})
	}
}
