package credential

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

const enrollmentTokenRandomLength = 32

// EnrollmentTokenGenerator creates opaque tokens and their storage hashes.
type EnrollmentTokenGenerator struct {
	random io.Reader
}

// NewEnrollmentTokenGenerator creates a generator with explicit randomness.
func NewEnrollmentTokenGenerator(
	argRandom io.Reader,
) *EnrollmentTokenGenerator {
	return &EnrollmentTokenGenerator{
		random: argRandom,
	}
}

// NewDefaultEnrollmentTokenGenerator creates a production token generator.
func NewDefaultEnrollmentTokenGenerator() *EnrollmentTokenGenerator {
	return NewEnrollmentTokenGenerator(rand.Reader)
}

// Generate creates a 256-bit token and its SHA-256 storage hash.
func (generator *EnrollmentTokenGenerator) Generate() (
	string,
	[sha256.Size]byte,
	error,
) {
	randomBytes := make([]byte, enrollmentTokenRandomLength)

	if _, err := io.ReadFull(generator.random, randomBytes); err != nil {
		return "", [sha256.Size]byte{}, fmt.Errorf(
			"read password enrollment token randomness: %w",
			err,
		)
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	tokenHash := HashEnrollmentToken(token)

	return token, tokenHash, nil
}

// HashEnrollmentToken hashes a presented opaque enrollment token.
func HashEnrollmentToken(argToken string) [sha256.Size]byte {
	return sha256.Sum256([]byte(argToken))
}
