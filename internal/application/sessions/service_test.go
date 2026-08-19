package sessions

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/session"
)

type recordingAuthenticator struct {
	user identity.User
	err  error
}

func (authenticator *recordingAuthenticator) Authenticate(
	argContext context.Context,
	argUsername string,
	argPassword []byte,
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
}

func (repository *recordingSessionRepository) Create(
	argContext context.Context,
	argSession session.Session,
) error {
	repository.createdSession = argSession

	return nil
}

func (repository *recordingSessionRepository) FindByTokenHash(
	argContext context.Context,
	argTokenHash [sha256.Size]byte,
) (session.Session, error) {
	return session.Session{}, session.ErrNotFound
}

func (repository *recordingSessionRepository) Touch(
	argContext context.Context,
	argTokenHash [sha256.Size]byte,
	argNow time.Time,
) error {
	return nil
}

func (repository *recordingSessionRepository) Revoke(
	argContext context.Context,
	argTokenHash [sha256.Size]byte,
	argNow time.Time,
) error {
	return nil
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
		repository,
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
}
