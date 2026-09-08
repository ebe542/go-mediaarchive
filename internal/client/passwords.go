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
	argContext context.Context,
	argAccessToken string,
	argUserID string,
) (PasswordEnrollment, error) {
	var enrollment PasswordEnrollment
	if err := client.doJSON(
		argContext,
		http.MethodPost,
		"/api/v1/users/"+url.PathEscape(argUserID)+"/password-enrollment",
		argAccessToken,
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
	argContext context.Context,
	argToken string,
	argPassword []byte,
) error {
	return client.doJSON(
		argContext,
		http.MethodPost,
		"/api/v1/auth/password-enrollments",
		"",
		struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}{Token: argToken, Password: string(argPassword)},
		http.StatusNoContent,
		nil,
		"complete password enrollment",
	)
}

// ChangePassword replaces the authenticated user's password.
func (client *Client) ChangePassword(
	argContext context.Context,
	argAccessToken string,
	argCurrentPassword []byte,
	argNewPassword []byte,
) error {
	return client.doJSON(
		argContext,
		http.MethodPut,
		"/api/v1/users/me/password",
		argAccessToken,
		struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}{
			CurrentPassword: string(argCurrentPassword),
			NewPassword:     string(argNewPassword),
		},
		http.StatusNoContent,
		nil,
		"change password",
	)
}
