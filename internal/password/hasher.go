// Package password provides password hashing and verification.
package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	defaultMemory      uint32 = 19 * 1024
	defaultIterations  uint32 = 2
	defaultParallelism uint8  = 1
	defaultSaltLength  uint32 = 16
	defaultKeyLength   uint32 = 32

	maximumMemory      uint32 = 64 * 1024
	maximumIterations  uint32 = 10
	maximumParallelism uint8  = 4
	maximumSaltLength  uint32 = 64
	maximumKeyLength   uint32 = 64
)

// ErrInvalidParameters indicates unsafe or unsupported hashing parameters.
var ErrInvalidParameters = errors.New("invalid password hashing parameters")

// ErrInvalidHash indicates a malformed or unsupported encoded password hash.
var ErrInvalidHash = errors.New("invalid password hash")

// Parameters defines Argon2id resource and output settings.
type Parameters struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// Hasher creates and verifies self-describing Argon2id password hashes.
type Hasher struct {
	parameters Parameters
	random     io.Reader
}

// NewHasher creates an Argon2id hasher with explicit dependencies.
func NewHasher(
	argParameters Parameters,
	argRandom io.Reader,
) *Hasher {
	return &Hasher{
		parameters: argParameters,
		random:     argRandom,
	}
}

// NewDefaultHasher creates a production Argon2id hasher.
func NewDefaultHasher() *Hasher {
	return NewHasher(
		Parameters{
			Memory:      defaultMemory,
			Iterations:  defaultIterations,
			Parallelism: defaultParallelism,
			SaltLength:  defaultSaltLength,
			KeyLength:   defaultKeyLength,
		},
		rand.Reader,
	)
}

// Hash creates a salted, self-describing Argon2id password hash.
func (hasher *Hasher) Hash(argPassword []byte) (string, error) {
	if err := validateParameters(hasher.parameters); err != nil {
		return "", err
	}

	salt := make([]byte, hasher.parameters.SaltLength)
	if _, err := io.ReadFull(hasher.random, salt); err != nil {
		return "", fmt.Errorf("read password salt: %w", err)
	}

	derivedKey := argon2.IDKey(
		argPassword,
		salt,
		hasher.parameters.Iterations,
		hasher.parameters.Memory,
		hasher.parameters.Parallelism,
		hasher.parameters.KeyLength,
	)

	return encodeHash(hasher.parameters, salt, derivedKey), nil
}

// Verify reports whether a password matches an encoded Argon2id hash.
func (hasher *Hasher) Verify(
	argPassword []byte,
	argEncodedHash string,
) (bool, error) {
	parameters, salt, expectedKey, err := parseHash(argEncodedHash)
	if err != nil {
		return false, err
	}

	actualKey := argon2.IDKey(
		argPassword,
		salt,
		parameters.Iterations,
		parameters.Memory,
		parameters.Parallelism,
		parameters.KeyLength,
	)

	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

func encodeHash(
	argParameters Parameters,
	argSalt []byte,
	argDerivedKey []byte,
) string {
	encoding := base64.RawStdEncoding

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argParameters.Memory,
		argParameters.Iterations,
		argParameters.Parallelism,
		encoding.EncodeToString(argSalt),
		encoding.EncodeToString(argDerivedKey),
	)
}

func parseHash(
	argEncodedHash string,
) (Parameters, []byte, []byte, error) {
	parts := strings.Split(argEncodedHash, "$")
	if len(parts) != 6 ||
		parts[0] != "" ||
		parts[1] != "argon2id" {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	expectedVersion := fmt.Sprintf("v=%d", argon2.Version)
	if parts[2] != expectedVersion {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	var parameters Parameters

	scannedValues, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&parameters.Memory,
		&parameters.Iterations,
		&parameters.Parallelism,
	)
	if err != nil || scannedValues != 3 {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	canonicalParameters := fmt.Sprintf(
		"m=%d,t=%d,p=%d",
		parameters.Memory,
		parameters.Iterations,
		parameters.Parallelism,
	)
	if parts[3] != canonicalParameters {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	encoding := base64.RawStdEncoding

	salt, err := encoding.DecodeString(parts[4])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	derivedKey, err := encoding.DecodeString(parts[5])
	if err != nil {
		return Parameters{}, nil, nil, ErrInvalidHash
	}

	parameters.SaltLength = uint32(len(salt))
	parameters.KeyLength = uint32(len(derivedKey))

	if err := validateParameters(parameters); err != nil {
		return Parameters{}, nil, nil, fmt.Errorf(
			"%w: %v",
			ErrInvalidHash,
			err,
		)
	}

	return parameters, salt, derivedKey, nil
}

func validateParameters(argParameters Parameters) error {
	if argParameters.Memory < 8*uint32(argParameters.Parallelism) ||
		argParameters.Memory > maximumMemory {
		return fmt.Errorf(
			"%w: unsupported memory cost",
			ErrInvalidParameters,
		)
	}

	if argParameters.Iterations == 0 ||
		argParameters.Iterations > maximumIterations {
		return fmt.Errorf(
			"%w: unsupported iteration count",
			ErrInvalidParameters,
		)
	}

	if argParameters.Parallelism == 0 ||
		argParameters.Parallelism > maximumParallelism {
		return fmt.Errorf(
			"%w: unsupported parallelism",
			ErrInvalidParameters,
		)
	}

	if argParameters.SaltLength == 0 ||
		argParameters.SaltLength > maximumSaltLength {
		return fmt.Errorf(
			"%w: unsupported salt length",
			ErrInvalidParameters,
		)
	}

	if argParameters.KeyLength == 0 ||
		argParameters.KeyLength > maximumKeyLength {
		return fmt.Errorf(
			"%w: unsupported key length",
			ErrInvalidParameters,
		)
	}

	return nil
}
