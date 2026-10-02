package client

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// Session is an authenticated server-side session returned once to a client.
type Session struct {
	AccessToken string    `json:"accessToken"`
	TokenType   string    `json:"tokenType"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Login authenticates a user and creates a server-side session.
func (client *Client) Login(
	ctx context.Context,
	username string,
	password []byte,
) (Session, error) {
	var session Session
	if err := client.doJSON(
		ctx,
		http.MethodPost,
		"/api/v1/auth/sessions",
		"",
		struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}{Username: username, Password: string(password)},
		http.StatusCreated,
		&session,
		"login",
	); err != nil {
		return Session{}, err
	}

	if session.AccessToken == "" ||
		!strings.EqualFold(session.TokenType, "Bearer") ||
		session.ExpiresAt.IsZero() {
		return Session{}, errors.New("validate login response: invalid session")
	}

	return session, nil
}

// Logout revokes the current server-side session.
func (client *Client) Logout(
	ctx context.Context,
	accessToken string,
) error {
	return client.doJSON(
		ctx,
		http.MethodDelete,
		"/api/v1/auth/sessions/current",
		accessToken,
		nil,
		http.StatusNoContent,
		nil,
		"logout",
	)
}
