package authentication

import (
	"context"
	"errors"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

type authenticationUserRepository struct {
	user              identity.User
	err               error
	requestedUsername string
}

func (repository *authenticationUserRepository) Create(
	argContext context.Context,
	argUser identity.User,
) error {
	return nil
}

func (repository *authenticationUserRepository) FindByID(
	argContext context.Context,
	argID string,
) (identity.User, error) {
	return identity.User{}, nil
}

func (repository *authenticationUserRepository) FindByUsername(
	argContext context.Context,
	argUsername string,
) (identity.User, error) {
	repository.requestedUsername = argUsername

	return repository.user, repository.err
}

func (repository *authenticationUserRepository) Update(
	argContext context.Context,
	argUser identity.User,
) error {
	return nil
}

type authenticationCredentialRepository struct {
	passwordCredential credential.PasswordCredential
	err                error
	calls              int
}

func (repository *authenticationCredentialRepository) FindByUserID(
	argContext context.Context,
	argUserID string,
) (credential.PasswordCredential, error) {
	repository.calls++

	return repository.passwordCredential, repository.err
}

type recordingPasswordVerifier struct {
	encodedHash string
	calls       int
	matches     bool
	err         error
}

func (verifier *recordingPasswordVerifier) Verify(
	argPassword []byte,
	argEncodedHash string,
) (bool, error) {
	verifier.calls++
	verifier.encodedHash = argEncodedHash

	return verifier.matches, verifier.err
}

func TestServiceUsesDummyHashForUnknownUser(t *testing.T) {
	const dummyHash = "$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA"

	userRepository := &authenticationUserRepository{
		err: identity.ErrUserNotFound,
	}
	credentialRepository := &authenticationCredentialRepository{}
	verifier := &recordingPasswordVerifier{}

	service := NewService(
		userRepository,
		credentialRepository,
		verifier,
		dummyHash,
	)

	_, err := service.Authenticate(
		context.Background(),
		"unknown_user",
		[]byte("synthetic passphrase"),
	)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf(
			"expected ErrInvalidCredentials, got %v",
			err,
		)
	}

	if verifier.calls != 1 {
		t.Fatalf(
			"expected one password verification, got %d",
			verifier.calls,
		)
	}
	if verifier.encodedHash != dummyHash {
		t.Fatal("expected password verification against dummy hash")
	}

	if credentialRepository.calls != 0 {
		t.Fatalf(
			"expected no credential lookup, got %d",
			credentialRepository.calls,
		)
	}
}

func TestServiceAuthenticatesWithoutRevealingAccountState(t *testing.T) {
	const dummyHash = "$argon2id$dummy"
	const storedHash = "$argon2id$stored"

	testCases := map[string]struct {
		active                 bool
		passwordMatches        bool
		credentialError        error
		expectedHash           string
		expectedFailure        bool
		expectedCredentialCall bool
	}{
		"valid credentials": {
			active:                 true,
			passwordMatches:        true,
			expectedHash:           storedHash,
			expectedCredentialCall: true,
		},
		"wrong password": {
			active:                 true,
			expectedHash:           storedHash,
			expectedFailure:        true,
			expectedCredentialCall: true,
		},
		"inactive user": {
			passwordMatches:        true,
			expectedHash:           storedHash,
			expectedFailure:        true,
			expectedCredentialCall: true,
		},
		"user without credential": {
			active:                 true,
			credentialError:        credential.ErrPasswordCredentialNotFound,
			expectedHash:           dummyHash,
			expectedFailure:        true,
			expectedCredentialCall: true,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			user := identity.User{
				ID:       "123e4567-e89b-12d3-a456-426614174000",
				Username: "known_user",
				Active:   testCase.active,
			}

			userRepository := &authenticationUserRepository{
				user: user,
			}
			credentialRepository := &authenticationCredentialRepository{
				passwordCredential: credential.PasswordCredential{
					UserID:       user.ID,
					PasswordHash: storedHash,
				},
				err: testCase.credentialError,
			}
			verifier := &recordingPasswordVerifier{
				matches: testCase.passwordMatches,
			}

			service := NewService(
				userRepository,
				credentialRepository,
				verifier,
				dummyHash,
			)

			authenticatedUser, err := service.Authenticate(
				context.Background(),
				" Known_User ",
				[]byte("synthetic passphrase"),
			)

			if testCase.expectedFailure {
				if !errors.Is(err, ErrInvalidCredentials) {
					t.Fatalf(
						"expected ErrInvalidCredentials, got %v",
						err,
					)
				}
			} else {
				if err != nil {
					t.Fatalf("authenticate user: %v", err)
				}
				if authenticatedUser != user {
					t.Fatalf(
						"expected user %#v, got %#v",
						user,
						authenticatedUser,
					)
				}
			}

			if userRepository.requestedUsername != "known_user" {
				t.Fatalf(
					"expected normalized username %q, got %q",
					"known_user",
					userRepository.requestedUsername,
				)
			}

			if verifier.calls != 1 {
				t.Fatalf(
					"expected one password verification, got %d",
					verifier.calls,
				)
			}
			if verifier.encodedHash != testCase.expectedHash {
				t.Fatalf(
					"expected hash %q, got %q",
					testCase.expectedHash,
					verifier.encodedHash,
				)
			}

			expectedCalls := 0
			if testCase.expectedCredentialCall {
				expectedCalls = 1
			}
			if credentialRepository.calls != expectedCalls {
				t.Fatalf(
					"expected %d credential lookups, got %d",
					expectedCalls,
					credentialRepository.calls,
				)
			}
		})
	}
}
