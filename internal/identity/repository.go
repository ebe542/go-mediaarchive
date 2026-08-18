package identity

import "errors"

// ErrUserNotFound indicates that no user exists for the requested identifier.
var ErrUserNotFound = errors.New("user not found")

// ErrUserConflict indicates that a user ID or username already exists.
var ErrUserConflict = errors.New("user conflict")
