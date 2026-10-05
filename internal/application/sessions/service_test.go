package sessions

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/authentication"
	"github.com/ebe542/go-mediaarchive/internal/audit"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/session"
)

type recordingAuthenticator struct {
	user identity.User
	err  error
}

func (authenticator *recordingAuthenticator) Authenticate(
	ctx context.Context,
	username string,
	password []byte,
) (identity.User, error) {
	return authenticator.user, authenticator.err
}

type deterministicTokenGenerator struct {
	token     string
	tokenHash [sha256.Size]byte
	err       error
}

func (generator *deterministicTokenGenerator) Generate() (
	string,
	[sha256.Size]byte,
	error,
) {
	return generator.token, generator.tokenHash, generator.err
}

type recordingSessionRepository struct {
	createdSession session.Session
	createdEvent   audit.Event
	createError    error
	storedSession  session.Session
	findError      error
	touchedHash    [sha256.Size]byte
	touchedAt      time.Time
	revokedHash    [sha256.Size]byte
	revokedAt      time.Time
	revokeCalls    int
	touchCalls     int
}

func (repository *recordingSessionRepository) CreateWithAudit(
	ctx context.Context,
	storedSession session.Session,
	event audit.Event,
) error {
	repository.createdSession = storedSession
	repository.createdEvent = event

	return repository.createError
}

func (repository *recordingSessionRepository) Create(
	ctx context.Context,
	session session.Session,
) error {
	repository.createdSession = session

	return nil
}

type recordingAuditAppender struct {
	event audit.Event
	err   error
}

func (appender *recordingAuditAppender) Append(
	ctx context.Context,
	event audit.Event,
) error {
	appender.event = event

	return appender.err
}

func auditEventID() string {
	return "823e4567-e89b-12d3-a456-426614174000"
}

func (repository *recordingSessionRepository) FindByTokenHash(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
) (session.Session, error) {
	return repository.storedSession, repository.findError
}

func (repository *recordingSessionRepository) Touch(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) error {
	repository.touchedHash = tokenHash
	repository.touchedAt = now
	repository.touchCalls++

	return nil
}

func (repository *recordingSessionRepository) Revoke(
	ctx context.Context,
	tokenHash [sha256.Size]byte,
	now time.Time,
) error {
	repository.revokeCalls++
	repository.revokedHash = tokenHash
	repository.revokedAt = now

	return nil
}

type recordingUserFinder struct {
	user identity.User
	err  error
}

func (finder *recordingUserFinder) FindByID(
	ctx context.Context,
	id string,
) (identity.User, error) {
	return finder.user, finder.err
}

func TestServiceCreatesAuthenticatedSession(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 0, 0, 0, time.UTC)
	user := identity.User{
		ID:       "123e4567-e89b-12d3-a456-426614174000",
		Username: "archive_admin",
		Role:     identity.RoleAdmin,
		Active:   true,
	}

	token := "opaque-session-token"
	tokenHash := sha256.Sum256([]byte(token))

	authenticator := &recordingAuthenticator{
		user: user,
	}
	tokenGenerator := &deterministicTokenGenerator{
		token:     token,
		tokenHash: tokenHash,
	}
	repository := &recordingSessionRepository{}

	service := NewService(
		authenticator,
		&recordingUserFinder{
			user: user,
		},
		repository,
		&recordingAuditAppender{},
		auditEventID,
		tokenGenerator,
		func() time.Time {
			return now
		},
		8*time.Hour,
		30*time.Minute,
	)

	createdSession, err := service.Create(
		context.Background(),
		"archive_admin",
		[]byte("synthetic passphrase"),
	)
	if err != nil {
		t.Fatalf("create authenticated session: %v", err)
	}

	if createdSession.AccessToken != token {
		t.Fatal("expected generated access token")
	}
	if createdSession.ExpiresAt != now.Add(8*time.Hour) {
		t.Fatalf(
			"expected expiration %v, got %v",
			now.Add(8*time.Hour),
			createdSession.ExpiresAt,
		)
	}

	if repository.createdSession.TokenHash != tokenHash {
		t.Fatal("expected only the generated token hash to be persisted")
	}
	if repository.createdSession.UserID != user.ID {
		t.Fatalf(
			"expected session user ID %q, got %q",
			user.ID,
			repository.createdSession.UserID,
		)
	}
	if repository.createdEvent.Type != audit.TypeSessionCreated ||
		repository.createdEvent.Outcome != audit.OutcomeSuccess ||
		repository.createdEvent.ActorID != user.ID ||
		repository.createdEvent.ActorUsername != user.Username ||
		repository.createdEvent.ActorRole != string(user.Role) ||
		repository.createdEvent.TargetType != audit.TargetSession {
		t.Fatalf("unexpected session creation audit event: %+v", repository.createdEvent)
	}
}

func TestServiceAuditsDeniedSessionCreation(t *testing.T) {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	appender := &recordingAuditAppender{}
	repository := &recordingSessionRepository{}
	service := NewService(
		&recordingAuthenticator{err: authentication.ErrInvalidCredentials},
		&recordingUserFinder{},
		repository,
		appender,
		auditEventID,
		&deterministicTokenGenerator{},
		func() time.Time { return now },
		8*time.Hour,
		30*time.Minute,
	)

	_, err := service.Create(
		context.Background(),
		"  Archive_User  ",
		[]byte("incorrect password"),
	)
	if !errors.Is(err, authentication.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	if appender.event.Type != audit.TypeSessionCreateDenied ||
		appender.event.Outcome != audit.OutcomeDenied ||
		appender.event.TargetType != audit.TargetUser ||
		appender.event.TargetName != "archive_user" ||
		appender.event.Reason != audit.ReasonInvalidCredentials ||
		appender.event.ActorID != "" {
		t.Fatalf("unexpected denied session audit event: %+v", appender.event)
	}
	if repository.createdSession != (session.Session{}) {
		t.Fatal("expected no session after denied authentication")
	}
}

func TestServiceFailsClosedWhenDeniedAuditCannotPersist(t *testing.T) {
	auditFailure := errors.New("synthetic audit failure")
	service := NewService(
		&recordingAuthenticator{err: authentication.ErrInvalidCredentials},
		&recordingUserFinder{},
		&recordingSessionRepository{},
		&recordingAuditAppender{err: auditFailure},
		auditEventID,
		&deterministicTokenGenerator{},
		func() time.Time {
			return time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
		},
		8*time.Hour,
		30*time.Minute,
	)

	_, err := service.Create(
		context.Background(),
		"malformed username!",
		[]byte("incorrect password"),
	)
	if !errors.Is(err, auditFailure) {
		t.Fatalf("expected audit failure, got %v", err)
	}
	if errors.Is(err, authentication.ErrInvalidCredentials) {
		t.Fatal("expected audit failure to replace the public credential denial")
	}
}
func TestServiceResolvesActiveSession(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 20, 0, 0, time.UTC)
	token := "opaque-session-token"
	tokenHash := session.HashToken(token)

	user := identity.User{
		ID:       "123e4567-e89b-12d3-a456-426614174000",
		Username: "archive_admin",
		Role:     identity.RoleAdmin,
		Active:   true,
	}

	storedSession, err := session.New(
		tokenHash,
		user.ID,
		now.Add(-20*time.Minute),
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create session fixture: %v", err)
	}

	repository := &recordingSessionRepository{
		storedSession: storedSession,
	}
	userFinder := &recordingUserFinder{
		user: user,
	}

	service := NewService(
		&recordingAuthenticator{},
		userFinder,
		repository,
		&recordingAuditAppender{},
		auditEventID,
		&deterministicTokenGenerator{},
		func() time.Time {
			return now
		},
		8*time.Hour,
		30*time.Minute,
	)

	resolvedUser, err := service.Resolve(
		context.Background(),
		token,
	)
	if err != nil {
		t.Fatalf("resolve active session: %v", err)
	}

	if resolvedUser != user {
		t.Fatalf(
			"expected user %#v, got %#v",
			user,
			resolvedUser,
		)
	}

	if repository.touchedHash != tokenHash {
		t.Fatal("expected resolved session hash to be touched")
	}
	if repository.touchedAt != now {
		t.Fatalf(
			"expected touch time %v, got %v",
			now,
			repository.touchedAt,
		)
	}
}

func TestServiceRevokesSessionIdempotently(t *testing.T) {
	now := time.Date(2026, time.August, 19, 10, 30, 0, 0, time.UTC)
	token := "opaque-session-token"
	tokenHash := session.HashToken(token)

	repository := &recordingSessionRepository{}

	service := NewService(
		&recordingAuthenticator{},
		&recordingUserFinder{},
		repository,
		&recordingAuditAppender{},
		auditEventID,
		&deterministicTokenGenerator{},
		func() time.Time {
			return now
		},
		8*time.Hour,
		30*time.Minute,
	)

	if err := service.Revoke(
		context.Background(),
		token,
	); err != nil {
		t.Fatalf("revoke session: %v", err)
	}

	if err := service.Revoke(
		context.Background(),
		token,
	); err != nil {
		t.Fatalf("revoke session again: %v", err)
	}

	if repository.revokeCalls != 2 {
		t.Fatalf(
			"expected two idempotent revoke calls, got %d",
			repository.revokeCalls,
		)
	}

	if repository.revokedHash != tokenHash {
		t.Fatal("expected hashed token to be revoked")
	}

	if repository.revokedAt != now {
		t.Fatalf(
			"expected revocation time %v, got %v",
			now,
			repository.revokedAt,
		)
	}
}

func TestServiceRejectsUnusableSessionsGenerically(t *testing.T) {
	now := time.Date(2026, time.August, 19, 11, 0, 0, 0, time.UTC)
	token := "opaque-session-token"
	tokenHash := session.HashToken(token)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	validSession, err := session.New(
		tokenHash,
		userID,
		now.Add(-10*time.Minute),
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create valid session fixture: %v", err)
	}

	expiredSession, err := session.New(
		tokenHash,
		userID,
		now.Add(-9*time.Hour),
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create expired session fixture: %v", err)
	}

	idleSession, err := session.New(
		tokenHash,
		userID,
		now.Add(-time.Hour),
		8*time.Hour,
	)
	if err != nil {
		t.Fatalf("create idle session fixture: %v", err)
	}

	revokedSession := validSession
	revokedSession.RevokedAt = now.Add(-time.Minute)

	activeUser := identity.User{
		ID:       userID,
		Username: "archive_admin",
		Active:   true,
	}
	inactiveUser := activeUser
	inactiveUser.Active = false

	testCases := map[string]struct {
		storedSession session.Session
		sessionError  error
		user          identity.User
		userError     error
	}{
		"unknown token": {
			sessionError: session.ErrNotFound,
		},
		"expired session": {
			storedSession: expiredSession,
			user:          activeUser,
		},
		"idle session": {
			storedSession: idleSession,
			user:          activeUser,
		},
		"revoked session": {
			storedSession: revokedSession,
			user:          activeUser,
		},
		"inactive user": {
			storedSession: validSession,
			user:          inactiveUser,
		},
		"missing user": {
			storedSession: validSession,
			userError:     identity.ErrUserNotFound,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			repository := &recordingSessionRepository{
				storedSession: testCase.storedSession,
				findError:     testCase.sessionError,
			}
			userFinder := &recordingUserFinder{
				user: testCase.user,
				err:  testCase.userError,
			}

			service := NewService(
				&recordingAuthenticator{},
				userFinder,
				repository,
				&recordingAuditAppender{},
				auditEventID,
				&deterministicTokenGenerator{},
				func() time.Time {
					return now
				},
				8*time.Hour,
				30*time.Minute,
			)

			_, err := service.Resolve(
				context.Background(),
				token,
			)
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf(
					"expected ErrUnauthenticated, got %v",
					err,
				)
			}

			if repository.touchCalls != 0 {
				t.Fatalf(
					"expected no touch call, got %d",
					repository.touchCalls,
				)
			}
		})
	}
}
