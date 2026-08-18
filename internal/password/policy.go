package password

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

const (
	minimumLength = 15
	maximumLength = 1024
)

// ErrInvalidPassword indicates that a password violates the input policy.
var ErrInvalidPassword = errors.New("invalid password")

// Validate checks password length and UTF-8 validity without changing its value.
func Validate(argPassword []byte) error {
	if !utf8.Valid(argPassword) {
		return fmt.Errorf(
			"%w: expected valid UTF-8",
			ErrInvalidPassword,
		)
	}

	passwordLength := utf8.RuneCount(argPassword)
	if passwordLength < minimumLength ||
		passwordLength > maximumLength {
		return fmt.Errorf(
			"%w: length must be between %d and %d characters",
			ErrInvalidPassword,
			minimumLength,
			maximumLength,
		)
	}

	return nil
}
