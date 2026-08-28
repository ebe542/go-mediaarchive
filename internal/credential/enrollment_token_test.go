package credential

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
)

func TestEnrollmentTokenGeneratorCreatesOpaqueTokenAndHash(t *testing.T) {
	randomBytes := bytes.Repeat([]byte{0x42}, enrollmentTokenRandomLength)
	generator := NewEnrollmentTokenGenerator(bytes.NewReader(randomBytes))

	token, tokenHash, err := generator.Generate()
	if err != nil {
		t.Fatalf("generate enrollment token: %v", err)
	}

	decodedToken, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode enrollment token: %v", err)
	}
	if !bytes.Equal(decodedToken, randomBytes) {
		t.Fatal("expected token to contain the supplied random bytes")
	}

	expectedHash := sha256.Sum256([]byte(token))
	if tokenHash != expectedHash {
		t.Fatalf("expected token hash %x, got %x", expectedHash, tokenHash)
	}
	if bytes.Contains([]byte(token), []byte("=")) {
		t.Fatal("expected unpadded URL-safe token")
	}
}

func TestEnrollmentTokenGeneratorReportsRandomnessFailure(t *testing.T) {
	expectedError := errors.New("randomness unavailable")
	generator := NewEnrollmentTokenGenerator(errorReader{err: expectedError})

	_, _, err := generator.Generate()
	if !errors.Is(err, expectedError) {
		t.Fatalf("expected randomness error, got %v", err)
	}
}

type errorReader struct {
	err error
}

func (reader errorReader) Read(_ []byte) (int, error) {
	return 0, reader.err
}
