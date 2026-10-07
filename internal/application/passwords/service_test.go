package passwords_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	passwords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

type recordingAuditAppender struct {
	event audit.Event
	err   error
}

func (appender *recordingAuditAppender) Append(
	_ context.Context,
	event audit.Event,
) error {
	appender.event = event

	return appender.err
}

func newPasswordService(
	users passwords.UserFinder,
	enrollments passwords.EnrollmentStore,
	tokens passwords.EnrollmentTokenGenerator,
	hasher passwords.PasswordHasher,
	clock passwords.Clock,
	lifetime time.Duration,
) *passwords.Service {
	return passwords.NewService(
		users,
		enrollments,
		tokens,
		hasher,
		clock,
		lifetime,
		&recordingAuditAppender{},
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)
}

func enrollmentAdministrator() identity.User {
	return identity.User{
		ID:       "723e4567-e89b-12d3-a456-426614174000",
		Username: "audit_admin",
		Role:     identity.RoleAdmin,
		Active:   true,
	}
}

func TestServiceIssuesPasswordEnrollment(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	token := "issued-enrollment-token"
	tokenHash := credential.HashEnrollmentToken(token)
	store := &recordingEnrollmentStore{}

	service := newPasswordService(
		&recordingEnrollmentUserFinder{user: user},
		store,
		&fixedEnrollmentTokenGenerator{token: token, tokenHash: tokenHash},
		&recordingPasswordHasher{},
		func() time.Time { return now },
		24*time.Hour)

	issued, err := service.IssueEnrollment(
		context.Background(),
		enrollmentAdministrator(),
		user.ID,
	)
	if err != nil {
		t.Fatalf("issue password enrollment: %v", err)
	}
	if issued.Token != token {
		t.Fatalf("expected token %q, got %q", token, issued.Token)
	}
	if !issued.ExpiresAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("unexpected expiration time %s", issued.ExpiresAt)
	}
	if store.savedEnrollment.UserID != user.ID ||
		store.savedEnrollment.TokenHash != tokenHash {
		t.Fatalf("unexpected stored enrollment %#v", store.savedEnrollment)
	}
	if store.savedEvent.Type != audit.TypePasswordEnrollmentIssued ||
		store.savedEvent.Outcome != audit.OutcomeSuccess ||
		store.savedEvent.ActorID != enrollmentAdministrator().ID ||
		store.savedEvent.TargetID != user.ID ||
		store.savedEvent.TargetName != user.Username ||
		store.savedEvent.Reason != "" {
		t.Fatalf("unexpected enrollment issue audit event: %#v", store.savedEvent)
	}
}

func TestServiceDoesNotGenerateTokenForMissingUser(t *testing.T) {
	expectedError := identity.ErrUserNotFound
	tokens := &fixedEnrollmentTokenGenerator{
		err: errors.New("token generator must not be called"),
	}
	service := newPasswordService(
		&recordingEnrollmentUserFinder{err: expectedError},
		&recordingEnrollmentStore{},
		tokens,
		&recordingPasswordHasher{},
		time.Now,
		24*time.Hour)

	_, err := service.IssueEnrollment(
		context.Background(),
		enrollmentAdministrator(),
		"123e4567-e89b-12d3-a456-426614174000",
	)
	if !errors.Is(err, expectedError) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
	if tokens.called {
		t.Fatal("expected token generator not to be called")
	}
}

func TestServicePropagatesExistingCredentialWhenIssuing(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	store := &recordingEnrollmentStore{
		saveErr: credential.ErrPasswordCredentialExists,
	}
	service := newPasswordService(
		&recordingEnrollmentUserFinder{user: user},
		store,
		&fixedEnrollmentTokenGenerator{
			token:     "issued-enrollment-token",
			tokenHash: credential.HashEnrollmentToken("issued-enrollment-token"),
		},
		&recordingPasswordHasher{},
		func() time.Time { return now },
		24*time.Hour)

	_, err := service.IssueEnrollment(
		context.Background(),
		enrollmentAdministrator(),
		user.ID,
	)
	if !errors.Is(err, credential.ErrPasswordCredentialExists) {
		t.Fatalf("expected ErrPasswordCredentialExists, got %v", err)
	}
}

func TestServiceAuditsDeniedEnrollmentIssue(t *testing.T) {
	now := time.Date(2026, time.October, 6, 13, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	testCases := map[string]struct {
		userID         string
		findError      error
		saveError      error
		expectedError  error
		expectedReason audit.Reason
		expectedTarget string
	}{
		"invalid user ID": {
			userID:         "not-a-user-id",
			findError:      identity.ErrInvalidUserID,
			expectedError:  identity.ErrInvalidUserID,
			expectedReason: audit.ReasonInvalidInput,
		},
		"unknown user": {
			userID:         user.ID,
			findError:      identity.ErrUserNotFound,
			expectedError:  identity.ErrUserNotFound,
			expectedReason: audit.ReasonUnknownTarget,
			expectedTarget: user.ID,
		},
		"existing credential": {
			userID:         user.ID,
			saveError:      credential.ErrPasswordCredentialExists,
			expectedError:  credential.ErrPasswordCredentialExists,
			expectedReason: audit.ReasonResourceConflict,
			expectedTarget: user.ID,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			appender := &recordingAuditAppender{}
			service := passwords.NewService(
				&recordingEnrollmentUserFinder{
					user: user,
					err:  testCase.findError,
				},
				&recordingEnrollmentStore{saveErr: testCase.saveError},
				&fixedEnrollmentTokenGenerator{
					token:     "issued-enrollment-token",
					tokenHash: credential.HashEnrollmentToken("issued-enrollment-token"),
				},
				&recordingPasswordHasher{},
				func() time.Time { return now },
				24*time.Hour,
				appender,
				func() string { return "823e4567-e89b-12d3-a456-426614174000" },
			)

			_, err := service.IssueEnrollment(
				context.Background(),
				enrollmentAdministrator(),
				testCase.userID,
			)
			if !errors.Is(err, testCase.expectedError) {
				t.Fatalf("expected %v, got %v", testCase.expectedError, err)
			}
			if appender.event.Type != audit.TypePasswordEnrollmentIssued ||
				appender.event.Outcome != audit.OutcomeDenied ||
				appender.event.Reason != testCase.expectedReason ||
				appender.event.TargetID != testCase.expectedTarget {
				t.Fatalf("unexpected denied enrollment issue event: %#v", appender.event)
			}
		})
	}
}

func TestServiceFailsClosedWhenEnrollmentIssueDenialAuditFails(t *testing.T) {
	now := time.Date(2026, time.October, 6, 13, 30, 0, 0, time.UTC)
	auditError := errors.New("audit storage unavailable")
	service := passwords.NewService(
		&recordingEnrollmentUserFinder{err: identity.ErrUserNotFound},
		&recordingEnrollmentStore{},
		&fixedEnrollmentTokenGenerator{},
		&recordingPasswordHasher{},
		func() time.Time { return now },
		24*time.Hour,
		&recordingAuditAppender{err: auditError},
		func() string { return "823e4567-e89b-12d3-a456-426614174000" },
	)

	_, err := service.IssueEnrollment(
		context.Background(),
		enrollmentAdministrator(),
		"123e4567-e89b-12d3-a456-426614174000",
	)
	if !errors.Is(err, auditError) {
		t.Fatalf("expected audit error, got %v", err)
	}
	if errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("expected audit failure to replace operation error, got %v", err)
	}
}

func TestServiceAllowsInactiveUserToReceiveEnrollment(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	inactiveUser, err := user.SetActive(false, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("deactivate user fixture: %v", err)
	}
	store := &recordingEnrollmentStore{}
	service := newPasswordService(
		&recordingEnrollmentUserFinder{user: inactiveUser},
		store,
		&fixedEnrollmentTokenGenerator{
			token:     "inactive-user-token",
			tokenHash: credential.HashEnrollmentToken("inactive-user-token"),
		},
		&recordingPasswordHasher{},
		func() time.Time { return now.Add(time.Minute) },
		24*time.Hour)

	if _, err := service.IssueEnrollment(
		context.Background(),
		enrollmentAdministrator(),
		inactiveUser.ID,
	); err != nil {
		t.Fatalf("issue inactive user enrollment: %v", err)
	}
	if store.savedEnrollment.UserID != inactiveUser.ID {
		t.Fatal("expected enrollment to be stored for inactive user")
	}
}

func TestServiceCompletesPasswordEnrollment(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	token := "valid-enrollment-token"
	passwordValue := []byte("correct horse battery staple")
	encodedHash := "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"
	enrollment, err := credential.NewPasswordEnrollment(
		user.ID,
		credential.HashEnrollmentToken(token),
		now.Add(-time.Hour),
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create enrollment fixture: %v", err)
	}
	store := &recordingEnrollmentStore{foundEnrollment: enrollment}
	hasher := &recordingPasswordHasher{encodedHash: encodedHash}
	service := newPasswordService(
		&recordingEnrollmentUserFinder{},
		store,
		&fixedEnrollmentTokenGenerator{},
		hasher,
		func() time.Time { return now },
		24*time.Hour)

	if err := service.CompleteEnrollment(
		context.Background(),
		token,
		passwordValue,
	); err != nil {
		t.Fatalf("complete password enrollment: %v", err)
	}
	if string(hasher.password) != string(passwordValue) {
		t.Fatal("expected password to be passed unchanged to hasher")
	}
	if store.consumedHash != enrollment.TokenHash {
		t.Fatal("expected enrollment token hash to be consumed")
	}
	if store.createdCredential.UserID != user.ID ||
		store.createdCredential.PasswordHash != encodedHash {
		t.Fatalf("unexpected created credential %#v", store.createdCredential)
	}
}

func TestServiceReturnsGenericErrorForUnusableEnrollment(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	expiredEnrollment, err := credential.NewPasswordEnrollment(
		user.ID,
		credential.HashEnrollmentToken("expired-token"),
		now.Add(-2*time.Hour),
		time.Hour,
	)
	if err != nil {
		t.Fatalf("create expired enrollment fixture: %v", err)
	}
	validEnrollment, err := credential.NewPasswordEnrollment(
		user.ID,
		credential.HashEnrollmentToken("raced-token"),
		now.Add(-time.Hour),
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create valid enrollment fixture: %v", err)
	}

	testCases := []struct {
		name  string
		store *recordingEnrollmentStore
	}{
		{
			name: "unknown or consumed token",
			store: &recordingEnrollmentStore{
				findErr: credential.ErrPasswordEnrollmentNotFound,
			},
		},
		{
			name:  "expired token",
			store: &recordingEnrollmentStore{foundEnrollment: expiredEnrollment},
		},
		{
			name: "replaced during completion",
			store: &recordingEnrollmentStore{
				foundEnrollment: validEnrollment,
				consumeErr:      credential.ErrPasswordEnrollmentNotFound,
			},
		},
		{
			name: "credential created concurrently",
			store: &recordingEnrollmentStore{
				foundEnrollment: validEnrollment,
				consumeErr:      credential.ErrPasswordCredentialExists,
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			service := newPasswordService(
				&recordingEnrollmentUserFinder{},
				testCase.store,
				&fixedEnrollmentTokenGenerator{},
				&recordingPasswordHasher{
					encodedHash: "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
				},
				func() time.Time { return now },
				24*time.Hour)

			err := service.CompleteEnrollment(
				context.Background(),
				"presented-token",
				[]byte("correct horse battery staple"),
			)
			if !errors.Is(err, passwords.ErrInvalidEnrollment) {
				t.Fatalf("expected ErrInvalidEnrollment, got %v", err)
			}
		})
	}
}

func TestServiceRejectsInvalidEnrolledPasswordBeforeHashing(t *testing.T) {
	now := time.Date(2026, time.August, 28, 10, 0, 0, 0, time.UTC)
	user := enrollmentUserFixture(t, now)
	enrollment, err := credential.NewPasswordEnrollment(
		user.ID,
		credential.HashEnrollmentToken("valid-token"),
		now.Add(-time.Hour),
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("create enrollment fixture: %v", err)
	}
	hasher := &recordingPasswordHasher{}
	store := &recordingEnrollmentStore{foundEnrollment: enrollment}
	service := newPasswordService(
		&recordingEnrollmentUserFinder{},
		store,
		&fixedEnrollmentTokenGenerator{},
		hasher,
		func() time.Time { return now },
		24*time.Hour)

	err = service.CompleteEnrollment(
		context.Background(),
		"valid-token",
		[]byte("too-short"),
	)
	if !errors.Is(err, password.ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
	if hasher.called {
		t.Fatal("expected invalid password not to be hashed")
	}
	if store.consumeCalled {
		t.Fatal("expected invalid password not to reach persistence")
	}
}

func enrollmentUserFixture(t *testing.T, now time.Time) identity.User {
	t.Helper()

	user, err := identity.NewUser(
		"123e4567-e89b-12d3-a456-426614174000",
		"enrollment_user",
		"Enrollment User",
		identity.RoleViewer,
		now,
	)
	if err != nil {
		t.Fatalf("create user fixture: %v", err)
	}

	return user
}

type recordingEnrollmentUserFinder struct {
	user identity.User
	err  error
}

func (finder *recordingEnrollmentUserFinder) FindByID(
	_ context.Context,
	_ string,
) (identity.User, error) {
	return finder.user, finder.err
}

type fixedEnrollmentTokenGenerator struct {
	token     string
	tokenHash [sha256.Size]byte
	err       error
	called    bool
}

func (generator *fixedEnrollmentTokenGenerator) Generate() (
	string,
	[sha256.Size]byte,
	error,
) {
	generator.called = true

	return generator.token, generator.tokenHash, generator.err
}

type recordingPasswordHasher struct {
	encodedHash string
	err         error
	password    []byte
	called      bool
}

func (hasher *recordingPasswordHasher) Hash(password []byte) (string, error) {
	hasher.called = true
	hasher.password = append([]byte(nil), password...)

	return hasher.encodedHash, hasher.err
}

type recordingEnrollmentStore struct {
	savedEnrollment   credential.PasswordEnrollment
	savedEvent        audit.Event
	foundEnrollment   credential.PasswordEnrollment
	createdCredential credential.PasswordCredential
	consumedHash      [sha256.Size]byte
	saveErr           error
	findErr           error
	consumeErr        error
	consumeCalled     bool
}

func (store *recordingEnrollmentStore) SaveForCredentiallessUserWithAudit(
	ctx context.Context,
	enrollment credential.PasswordEnrollment,
	event audit.Event,
) error {
	store.savedEvent = event

	return store.SaveForCredentiallessUser(ctx, enrollment)
}

func (store *recordingEnrollmentStore) SaveForCredentiallessUser(
	_ context.Context,
	enrollment credential.PasswordEnrollment,
) error {
	store.savedEnrollment = enrollment

	return store.saveErr
}

func (store *recordingEnrollmentStore) FindByTokenHash(
	_ context.Context,
	_ [sha256.Size]byte,
) (credential.PasswordEnrollment, error) {
	return store.foundEnrollment, store.findErr
}

func (store *recordingEnrollmentStore) CreateCredentialAndConsume(
	_ context.Context,
	tokenHash [sha256.Size]byte,
	credential credential.PasswordCredential,
	_ time.Time,
) error {
	store.consumeCalled = true
	store.consumedHash = tokenHash
	store.createdCredential = credential

	return store.consumeErr
}
