package passwords

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

type recordingPasswordChangeRepository struct {
	credential  credential.PasswordCredential
	findError   error
	changed     credential.PasswordCredential
	changeError error
}

func (repository *recordingPasswordChangeRepository) FindByUserID(
	_ context.Context,
	_ string,
) (credential.PasswordCredential, error) {
	return repository.credential, repository.findError
}

func (repository *recordingPasswordChangeRepository) ChangePasswordAndRevokeSessions(
	_ context.Context,
	argCredential credential.PasswordCredential,
) error {
	repository.changed = argCredential

	return repository.changeError
}

type recordingPasswordVerifierHasher struct {
	matches       bool
	verifyError   error
	hash          string
	hashError     error
	verifiedValue []byte
	hashedValue   []byte
}

func (hasher *recordingPasswordVerifierHasher) Verify(
	argPassword []byte,
	_ string,
) (bool, error) {
	hasher.verifiedValue = append([]byte(nil), argPassword...)

	return hasher.matches, hasher.verifyError
}

func (hasher *recordingPasswordVerifierHasher) Hash(
	argPassword []byte,
) (string, error) {
	hasher.hashedValue = append([]byte(nil), argPassword...)

	return hasher.hash, hasher.hashError
}

func TestChangeServiceReplacesPasswordAndRevokesSessions(t *testing.T) {
	createdAt := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	changedAt := createdAt.Add(time.Hour)
	repository := &recordingPasswordChangeRepository{
		credential: passwordChangeCredential(t, createdAt),
	}
	hasher := &recordingPasswordVerifierHasher{
		matches: true,
		hash:    "$argon2id$new-hash",
	}
	service := NewChangeService(repository, hasher, func() time.Time {
		return changedAt
	})

	err := service.ChangePassword(
		context.Background(),
		repository.credential.UserID,
		[]byte("current synthetic passphrase"),
		[]byte("new synthetic passphrase"),
	)
	if err != nil {
		t.Fatalf("change password: %v", err)
	}

	if string(hasher.verifiedValue) != "current synthetic passphrase" {
		t.Fatal("expected exact current password to be verified")
	}
	if string(hasher.hashedValue) != "new synthetic passphrase" {
		t.Fatal("expected exact new password to be hashed")
	}
	if repository.changed.PasswordHash != "$argon2id$new-hash" ||
		repository.changed.CreatedAt != createdAt ||
		repository.changed.UpdatedAt != changedAt {
		t.Fatalf("unexpected changed credential: %+v", repository.changed)
	}
}

func TestChangeServiceRejectsInvalidCurrentPassword(t *testing.T) {
	repository := &recordingPasswordChangeRepository{
		credential: passwordChangeCredential(
			t,
			time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC),
		),
	}
	hasher := &recordingPasswordVerifierHasher{matches: false}
	service := NewChangeService(repository, hasher, time.Now)

	err := service.ChangePassword(
		context.Background(),
		repository.credential.UserID,
		[]byte("incorrect synthetic passphrase"),
		[]byte("new synthetic passphrase"),
	)
	if !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("expected ErrInvalidCurrentPassword, got %v", err)
	}
	if repository.changed.UserID != "" || len(hasher.hashedValue) != 0 {
		t.Fatal("expected rejected password not to be persisted or hashed")
	}
}

func TestChangeServiceRejectsInvalidOrUnchangedNewPassword(t *testing.T) {
	testCases := []struct {
		name        string
		current     string
		newPassword string
		expectedErr error
	}{
		{
			name:        "invalid new password",
			current:     "current synthetic passphrase",
			newPassword: "short",
			expectedErr: password.ErrInvalidPassword,
		},
		{
			name:        "unchanged password",
			current:     "same synthetic passphrase",
			newPassword: "same synthetic passphrase",
			expectedErr: ErrPasswordUnchanged,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			repository := &recordingPasswordChangeRepository{
				credential: passwordChangeCredential(
					t,
					time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC),
				),
			}
			hasher := &recordingPasswordVerifierHasher{matches: true}
			service := NewChangeService(repository, hasher, time.Now)

			err := service.ChangePassword(
				context.Background(),
				repository.credential.UserID,
				[]byte(testCase.current),
				[]byte(testCase.newPassword),
			)
			if !errors.Is(err, testCase.expectedErr) {
				t.Fatalf("expected %v, got %v", testCase.expectedErr, err)
			}
			if repository.changed.UserID != "" || len(hasher.hashedValue) != 0 {
				t.Fatal("expected invalid new password not to be persisted or hashed")
			}
		})
	}
}

func passwordChangeCredential(
	t *testing.T,
	argCreatedAt time.Time,
) credential.PasswordCredential {
	t.Helper()

	storedCredential, err := credential.NewPasswordCredential(
		"123e4567-e89b-12d3-a456-426614174000",
		"$argon2id$stored-hash",
		argCreatedAt,
	)
	if err != nil {
		t.Fatalf("create password credential fixture: %v", err)
	}

	return storedCredential
}
