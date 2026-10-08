package passwords

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

type recordingPasswordChangeRepository struct {
	credential   credential.PasswordCredential
	findError    error
	changed      credential.PasswordCredential
	changedEvent audit.Event
	changeError  error
}

func (repository *recordingPasswordChangeRepository) FindByUserID(
	_ context.Context,
	_ string,
) (credential.PasswordCredential, error) {
	return repository.credential, repository.findError
}

func (repository *recordingPasswordChangeRepository) ChangePasswordAndRevokeSessions(
	_ context.Context,
	credential credential.PasswordCredential,
) error {
	repository.changed = credential

	return repository.changeError
}

func (repository *recordingPasswordChangeRepository) ChangePasswordAndRevokeSessionsWithAudit(
	_ context.Context,
	passwordCredential credential.PasswordCredential,
	event audit.Event,
) error {
	repository.changed = passwordCredential
	repository.changedEvent = event

	return repository.changeError
}

type passwordChangeAuditAppender struct {
	event audit.Event
	err   error
}

func (appender *passwordChangeAuditAppender) Append(
	_ context.Context,
	event audit.Event,
) error {
	appender.event = event

	return appender.err
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
	password []byte,
	_ string,
) (bool, error) {
	hasher.verifiedValue = append([]byte(nil), password...)

	return hasher.matches, hasher.verifyError
}

func (hasher *recordingPasswordVerifierHasher) Hash(
	password []byte,
) (string, error) {
	hasher.hashedValue = append([]byte(nil), password...)

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
	service := newPasswordChangeService(
		repository,
		hasher,
		&passwordChangeAuditAppender{},
		func() time.Time { return changedAt },
	)
	actor := passwordChangeActor(t, createdAt)

	err := service.ChangePassword(
		context.Background(),
		actor,
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
	if repository.changedEvent.Type != audit.TypePasswordChanged ||
		repository.changedEvent.Outcome != audit.OutcomeSuccess ||
		repository.changedEvent.ActorID != actor.ID ||
		repository.changedEvent.TargetID != actor.ID ||
		repository.changedEvent.TargetName != actor.Username {
		t.Fatalf("unexpected password change audit event: %+v", repository.changedEvent)
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
	appender := &passwordChangeAuditAppender{}
	service := newPasswordChangeService(repository, hasher, appender, time.Now)
	actor := passwordChangeActor(t, repository.credential.CreatedAt)

	err := service.ChangePassword(
		context.Background(),
		actor,
		[]byte("incorrect synthetic passphrase"),
		[]byte("new synthetic passphrase"),
	)
	if !errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("expected ErrInvalidCurrentPassword, got %v", err)
	}
	if repository.changed.UserID != "" || len(hasher.hashedValue) != 0 {
		t.Fatal("expected rejected password not to be persisted or hashed")
	}
	if appender.event.Outcome != audit.OutcomeDenied ||
		appender.event.Reason != audit.ReasonInvalidCredentials {
		t.Fatalf("unexpected password change denial: %+v", appender.event)
	}
}

func TestChangeServiceRejectsInvalidOrUnchangedNewPassword(t *testing.T) {
	testCases := []struct {
		name           string
		current        string
		newPassword    string
		expectedErr    error
		expectedReason audit.Reason
	}{
		{
			name:           "invalid new password",
			current:        "current synthetic passphrase",
			newPassword:    "short",
			expectedErr:    password.ErrInvalidPassword,
			expectedReason: audit.ReasonInvalidInput,
		},
		{
			name:           "unchanged password",
			current:        "same synthetic passphrase",
			newPassword:    "same synthetic passphrase",
			expectedErr:    ErrPasswordUnchanged,
			expectedReason: audit.ReasonResourceConflict,
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
			appender := &passwordChangeAuditAppender{}
			service := newPasswordChangeService(repository, hasher, appender, time.Now)
			actor := passwordChangeActor(t, repository.credential.CreatedAt)

			err := service.ChangePassword(
				context.Background(),
				actor,
				[]byte(testCase.current),
				[]byte(testCase.newPassword),
			)
			if !errors.Is(err, testCase.expectedErr) {
				t.Fatalf("expected %v, got %v", testCase.expectedErr, err)
			}
			if repository.changed.UserID != "" || len(hasher.hashedValue) != 0 {
				t.Fatal("expected invalid new password not to be persisted or hashed")
			}
			if appender.event.Outcome != audit.OutcomeDenied ||
				appender.event.Reason != testCase.expectedReason {
				t.Fatalf("unexpected password change denial: %+v", appender.event)
			}
		})
	}
}

func TestChangeServiceFailsClosedWhenDenialCannotBeAudited(t *testing.T) {
	auditError := errors.New("synthetic audit failure")
	createdAt := time.Date(2026, time.August, 28, 9, 0, 0, 0, time.UTC)
	repository := &recordingPasswordChangeRepository{
		credential: passwordChangeCredential(t, createdAt),
	}
	service := newPasswordChangeService(
		repository,
		&recordingPasswordVerifierHasher{matches: false},
		&passwordChangeAuditAppender{err: auditError},
		time.Now,
	)

	err := service.ChangePassword(
		context.Background(),
		passwordChangeActor(t, createdAt),
		[]byte("incorrect synthetic passphrase"),
		[]byte("new synthetic passphrase"),
	)
	if !errors.Is(err, auditError) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if errors.Is(err, ErrInvalidCurrentPassword) {
		t.Fatalf("expected audit failure to hide the denial, got %v", err)
	}
}

func newPasswordChangeService(
	repository PasswordChangeRepository,
	hasher PasswordVerifierHasher,
	appender audit.Appender,
	clock Clock,
) *ChangeService {
	return NewChangeService(
		repository,
		hasher,
		appender,
		func() string { return "a23e4567-e89b-12d3-a456-426614174000" },
		clock,
	)
}

func passwordChangeActor(t *testing.T, createdAt time.Time) identity.User {
	t.Helper()

	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"password_change_user",
		"Password Change User",
		identity.RoleViewer,
		createdAt,
	)
	if err != nil {
		t.Fatalf("create password change actor: %v", err)
	}

	return user
}

func passwordChangeCredential(
	t *testing.T,
	createdAt time.Time,
) credential.PasswordCredential {
	t.Helper()

	storedCredential, err := credential.NewPasswordCredential(
		"123e4567-e89b-12d3-a456-426614174000",
		"$argon2id$stored-hash",
		createdAt,
	)
	if err != nil {
		t.Fatalf("create password credential fixture: %v", err)
	}

	return storedCredential
}
