package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

type recordingHasher struct {
	receivedPassword []byte
	encodedHash      string
	hashError        error
	calls            int
}

func (hasher *recordingHasher) Hash(
	argPassword []byte,
) (string, error) {
	hasher.calls++
	hasher.receivedPassword = append([]byte(nil), argPassword...)

	return hasher.encodedHash, hasher.hashError
}

type recordingBootstrapper struct {
	user       identity.User
	credential credential.PasswordCredential
	err        error
	calls      int
}

func (bootstrapper *recordingBootstrapper) BootstrapAdmin(
	argContext context.Context,
	argUser identity.User,
	argCredential credential.PasswordCredential,
) error {
	bootstrapper.calls++
	bootstrapper.user = argUser
	bootstrapper.credential = argCredential

	return bootstrapper.err
}

func TestServiceBootstrapsAdministrator(t *testing.T) {
	t.Parallel()

	const generatedID = "123e4567-e89b-12d3-a456-426614174000"
	const encodedHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"

	currentTime := time.Date(
		2026,
		time.August,
		18,
		22,
		0,
		0,
		0,
		time.FixedZone("test", 2*60*60),
	)
	plainPassword := []byte("synthetic passphrase")

	hasher := &recordingHasher{
		encodedHash: encodedHash,
	}
	bootstrapper := &recordingBootstrapper{}

	service := NewService(
		bootstrapper,
		hasher,
		func() string {
			return generatedID
		},
		func() time.Time {
			return currentTime
		},
	)

	adminUser, err := service.BootstrapAdmin(
		context.Background(),
		Input{
			Username:    " Bootstrap_Admin ",
			DisplayName: " Bootstrap Administrator ",
			Password:    plainPassword,
		},
	)
	if err != nil {
		t.Fatalf("bootstrap administrator: %v", err)
	}

	expectedUser := identity.User{
		ID:          generatedID,
		Username:    "bootstrap_admin",
		DisplayName: "Bootstrap Administrator",
		Role:        identity.RoleAdmin,
		Active:      true,
		CreatedAt:   currentTime.UTC(),
		UpdatedAt:   currentTime.UTC(),
	}

	if adminUser != expectedUser {
		t.Fatalf(
			"expected administrator %#v, got %#v",
			expectedUser,
			adminUser,
		)
	}

	if bootstrapper.user != expectedUser {
		t.Fatalf(
			"expected persisted administrator %#v, got %#v",
			expectedUser,
			bootstrapper.user,
		)
	}

	expectedCredential := credential.PasswordCredential{
		UserID:       generatedID,
		PasswordHash: encodedHash,
		CreatedAt:    currentTime.UTC(),
		UpdatedAt:    currentTime.UTC(),
	}

	if bootstrapper.credential != expectedCredential {
		t.Fatalf(
			"expected credential %#v, got %#v",
			expectedCredential,
			bootstrapper.credential,
		)
	}

	if string(hasher.receivedPassword) != string(plainPassword) {
		t.Fatal("expected hasher to receive the supplied password")
	}
}

func TestServiceRejectsInvalidPasswordBeforeHashing(t *testing.T) {
	t.Parallel()

	hasher := &recordingHasher{}
	bootstrapper := &recordingBootstrapper{}

	service := NewService(
		bootstrapper,
		hasher,
		func() string {
			return "123e4567-e89b-12d3-a456-426614174000"
		},
		func() time.Time {
			return time.Date(
				2026,
				time.August,
				18,
				22,
				0,
				0,
				0,
				time.UTC,
			)
		},
	)

	_, err := service.BootstrapAdmin(
		context.Background(),
		Input{
			Username:    "bootstrap_admin",
			DisplayName: "Bootstrap Administrator",
			Password:    []byte("too-short"),
		},
	)
	if !errors.Is(err, password.ErrInvalidPassword) {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}

	if hasher.calls != 0 {
		t.Fatalf("expected no hash call, got %d", hasher.calls)
	}

	if bootstrapper.calls != 0 {
		t.Fatalf(
			"expected no persistence call, got %d",
			bootstrapper.calls,
		)
	}
}

func TestServicePreservesAlreadyBootstrappedError(t *testing.T) {
	t.Parallel()

	const encodedHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"

	hasher := &recordingHasher{
		encodedHash: encodedHash,
	}
	bootstrapper := &recordingBootstrapper{
		err: credential.ErrAlreadyBootstrapped,
	}

	service := NewService(
		bootstrapper,
		hasher,
		func() string {
			return "123e4567-e89b-12d3-a456-426614174000"
		},
		func() time.Time {
			return time.Date(
				2026,
				time.August,
				18,
				22,
				0,
				0,
				0,
				time.UTC,
			)
		},
	)

	_, err := service.BootstrapAdmin(
		context.Background(),
		Input{
			Username:    "bootstrap_admin",
			DisplayName: "Bootstrap Administrator",
			Password:    []byte("synthetic passphrase"),
		},
	)
	if !errors.Is(err, credential.ErrAlreadyBootstrapped) {
		t.Fatalf(
			"expected ErrAlreadyBootstrapped, got %v",
			err,
		)
	}
}
