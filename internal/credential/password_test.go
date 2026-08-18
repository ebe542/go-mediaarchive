package credential

import (
	"errors"
	"testing"
	"time"
)

func TestNewPasswordCredentialCreatesValidatedCredential(t *testing.T) {
	t.Parallel()

	now := time.Date(
		2026,
		time.August,
		18,
		20,
		0,
		0,
		0,
		time.FixedZone("test", 2*60*60),
	)

	const userID = "123e4567-e89b-12d3-a456-426614174000"
	const encodedHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"

	passwordCredential, err := NewPasswordCredential(
		userID,
		encodedHash,
		now,
	)
	if err != nil {
		t.Fatalf("create password credential: %v", err)
	}

	expectedCredential := PasswordCredential{
		UserID:       userID,
		PasswordHash: encodedHash,
		CreatedAt:    now.UTC(),
		UpdatedAt:    now.UTC(),
	}

	if passwordCredential != expectedCredential {
		t.Fatalf(
			"expected credential %#v, got %#v",
			expectedCredential,
			passwordCredential,
		)
	}
}

func TestNewPasswordCredentialRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	validTime := time.Date(
		2026,
		time.August,
		18,
		20,
		0,
		0,
		0,
		time.UTC,
	)

	testCases := map[string]struct {
		userID       string
		passwordHash string
		now          time.Time
		expectedErr  error
	}{
		"invalid user ID": {
			userID:       "not-a-uuid",
			passwordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
			now:          validTime,
			expectedErr:  ErrInvalidUserID,
		},
		"uppercase user ID": {
			userID:       "123E4567-E89B-12D3-A456-426614174000",
			passwordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
			now:          validTime,
			expectedErr:  ErrInvalidUserID,
		},
		"unsupported password hash": {
			userID:       "123e4567-e89b-12d3-a456-426614174000",
			passwordHash: "plaintext-password",
			now:          validTime,
			expectedErr:  ErrInvalidPasswordHash,
		},
		"zero timestamp": {
			userID:       "123e4567-e89b-12d3-a456-426614174000",
			passwordHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
			now:          time.Time{},
			expectedErr:  ErrInvalidTimestamp,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewPasswordCredential(
				testCase.userID,
				testCase.passwordHash,
				testCase.now,
			)
			if !errors.Is(err, testCase.expectedErr) {
				t.Fatalf(
					"expected %v, got %v",
					testCase.expectedErr,
					err,
				)
			}
		})
	}
}
