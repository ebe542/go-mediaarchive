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
	argContext context.Context,
	argUsername string,
	argPassword []byte,
) (Session, error) {
	var session Session
	if err := client.doJSON(
		argContext,
		http.MethodPost,
		"/api/v1/auth/sessions",
		"",
		struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}{Username: argUsername, Password: string(argPassword)},
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
	argContext context.Context,
	argAccessToken string,
) error {
	return client.doJSON(
		argContext,
		http.MethodDelete,
		"/api/v1/auth/sessions/current",
		argAccessToken,
		nil,
		http.StatusNoContent,
		nil,
		"logout",
	)
}
