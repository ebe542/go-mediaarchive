package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// PasswordEnrollment is the one-time secret issued to an administrator.
type PasswordEnrollment struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// IssuePasswordEnrollment creates or replaces a user's enrollment token.
func (client *Client) IssuePasswordEnrollment(
	ctx context.Context,
	accessToken string,
	userID string,
) (PasswordEnrollment, error) {
	var enrollment PasswordEnrollment
	if err := client.doJSON(
		ctx,
		http.MethodPost,
		"/api/v1/users/"+url.PathEscape(userID)+"/password-enrollment",
		accessToken,
		nil,
		http.StatusCreated,
		&enrollment,
		"issue password enrollment",
	); err != nil {
		return PasswordEnrollment{}, err
	}
	if enrollment.Token == "" || enrollment.ExpiresAt.IsZero() {
		return PasswordEnrollment{}, errors.New(
			"validate password enrollment response: enrollment is incomplete",
		)
	}

	return enrollment, nil
}

// CompletePasswordEnrollment creates an initial password credential.
func (client *Client) CompletePasswordEnrollment(
	ctx context.Context,
	token string,
	password []byte,
) error {
	return client.doJSON(
		ctx,
		http.MethodPost,
		"/api/v1/auth/password-enrollments",
		"",
		struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}{Token: token, Password: string(password)},
		http.StatusNoContent,
		nil,
		"complete password enrollment",
	)
}

// ChangePassword replaces the authenticated user's password.
func (client *Client) ChangePassword(
	ctx context.Context,
	accessToken string,
	currentPassword []byte,
	newPassword []byte,
) error {
	return client.doJSON(
		ctx,
		http.MethodPut,
		"/api/v1/users/me/password",
		accessToken,
		struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}{
			CurrentPassword: string(currentPassword),
			NewPassword:     string(newPassword),
		},
		http.StatusNoContent,
		nil,
		"change password",
	)
}
