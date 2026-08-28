package credential

import (
	"context"
	"crypto/sha256"
	"errors"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// ErrAlreadyBootstrapped indicates that an initial user already exists.
var ErrAlreadyBootstrapped = errors.New("administrator bootstrap already completed")

// AdminBootstrapper atomically stores the first administrator and credential.
type AdminBootstrapper interface {
	BootstrapAdmin(
		argContext context.Context,
		argUser identity.User,
		argCredential PasswordCredential,
	) error
}

// ErrPasswordCredentialNotFound indicates that a user has no password credential.
var ErrPasswordCredentialNotFound = errors.New(
	"password credential not found",
)

// ErrPasswordCredentialExists indicates that a user already has a password.
var ErrPasswordCredentialExists = errors.New(
	"password credential already exists",
)

// PasswordCredentialRepository loads password credentials for authentication.
type PasswordCredentialRepository interface {
	FindByUserID(
		argContext context.Context,
		argUserID string,
	) (PasswordCredential, error)
}

// ErrPasswordEnrollmentNotFound indicates that no enrollment matches a token.
var ErrPasswordEnrollmentNotFound = errors.New(
	"password enrollment not found",
)

// PasswordEnrollmentRepository stores replaceable password enrollments.
type PasswordEnrollmentRepository interface {
	SaveForCredentiallessUser(
		argContext context.Context,
		argEnrollment PasswordEnrollment,
	) error

	FindByTokenHash(
		argContext context.Context,
		argTokenHash [sha256.Size]byte,
	) (PasswordEnrollment, error)

	CreateCredentialAndConsume(
		argContext context.Context,
		argTokenHash [sha256.Size]byte,
		argCredential PasswordCredential,
		argNow time.Time,
	) error
}
