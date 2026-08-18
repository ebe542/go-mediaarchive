package credential

import (
	"context"
	"errors"

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
