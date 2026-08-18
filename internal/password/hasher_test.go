package password

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestHasherHashesAndVerifiesPassword(t *testing.T) {

	parameters := Parameters{
		Memory:      19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}

	salt := bytes.Repeat([]byte{0x42}, int(parameters.SaltLength))
	hasher := NewHasher(parameters, bytes.NewReader(salt))
	plainPassword := []byte("synthetic-test-password")

	encodedHash, err := hasher.Hash(plainPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	expectedPrefix := "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(encodedHash, expectedPrefix) {
		t.Fatalf(
			"expected hash prefix %q, got %q",
			expectedPrefix,
			encodedHash,
		)
	}

	matches, err := hasher.Verify(plainPassword, encodedHash)
	if err != nil {
		t.Fatalf("verify correct password: %v", err)
	}
	if !matches {
		t.Fatal("expected correct password to match")
	}

	matches, err = hasher.Verify(
		[]byte("different-synthetic-password"),
		encodedHash,
	)
	if err != nil {
		t.Fatalf("verify wrong password: %v", err)
	}
	if matches {
		t.Fatal("expected wrong password not to match")
	}
}

func TestHasherRejectsMalformedHashes(t *testing.T) {

	parameters := Parameters{
		Memory:      19 * 1024,
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	}

	salt := bytes.Repeat([]byte{0x42}, int(parameters.SaltLength))
	hasher := NewHasher(parameters, bytes.NewReader(salt))

	validHash, err := hasher.Hash([]byte("synthetic-test-password"))
	if err != nil {
		t.Fatalf("create valid hash fixture: %v", err)
	}

	parts := strings.Split(validHash, "$")
	invalidBase64Hash := strings.Join(
		[]string{
			parts[0],
			parts[1],
			parts[2],
			parts[3],
			"invalid!",
			parts[5],
		},
		"$",
	)

	testCases := map[string]string{
		"empty": "",
		"wrong algorithm": strings.Replace(
			validHash,
			"$argon2id$",
			"$argon2i$",
			1,
		),
		"unsupported version": strings.Replace(
			validHash,
			"$v=19$",
			"$v=18$",
			1,
		),
		"excessive memory": strings.Replace(
			validHash,
			"m=19456",
			"m=999999",
			1,
		),
		"trailing parameter data": strings.Replace(
			validHash,
			"p=1$",
			"p=1extra$",
			1,
		),
		"invalid salt encoding": invalidBase64Hash,
	}

	for name, encodedHash := range testCases {
		t.Run(name, func(t *testing.T) {

			matches, err := hasher.Verify(
				[]byte("synthetic-test-password"),
				encodedHash,
			)
			if !errors.Is(err, ErrInvalidHash) {
				t.Fatalf("expected ErrInvalidHash, got %v", err)
			}
			if matches {
				t.Fatal("expected malformed hash not to match")
			}
		})
	}
}

func TestValidate(t *testing.T) {

	testCases := map[string]struct {
		password      []byte
		expectedError bool
	}{
		"minimum length": {
			password: []byte("123456789012345"),
		},
		"Unicode code points": {
			password: []byte("äöü界界界界界界界界界界界界"),
		},
		"too short": {
			password:      []byte("12345678901234"),
			expectedError: true,
		},
		"invalid UTF-8": {
			password:      []byte{0xff, 0xfe, 0xfd},
			expectedError: true,
		},
		"too long": {
			password:      bytes.Repeat([]byte("a"), 1025),
			expectedError: true,
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {

			err := Validate(testCase.password)

			if testCase.expectedError && !errors.Is(err, ErrInvalidPassword) {
				t.Fatalf("expected ErrInvalidPassword, got %v", err)
			}
			if !testCase.expectedError && err != nil {
				t.Fatalf("expected valid password, got %v", err)
			}
		})
	}
}
