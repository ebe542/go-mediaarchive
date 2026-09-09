package client

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// User is the public API representation of a user identity.
type User struct {
	ID          string        `json:"id"`
	Username    string        `json:"username"`
	DisplayName string        `json:"displayName"`
	Role        identity.Role `json:"role"`
	Active      bool          `json:"active"`
	CreatedAt   time.Time     `json:"createdAt"`
	UpdatedAt   time.Time     `json:"updatedAt"`
}

// UserInput contains administrator-controlled public user details.
type UserInput struct {
	Username    string        `json:"username"`
	DisplayName string        `json:"displayName"`
	Role        identity.Role `json:"role"`
}

// UserPage is a bounded user directory response.
type UserPage struct {
	Users      []User `json:"users"`
	NextCursor string `json:"nextCursor,omitempty"`
}

// CurrentUser returns the identity represented by an access token.
func (client *Client) CurrentUser(
	argContext context.Context,
	argAccessToken string,
) (User, error) {
	return client.userByPath(
		argContext,
		argAccessToken,
		"/api/v1/users/me",
		"current user",
	)
}

// UserByID returns a user identity by stable ID.
func (client *Client) UserByID(
	argContext context.Context,
	argAccessToken string,
	argID string,
) (User, error) {
	return client.userByPath(
		argContext,
		argAccessToken,
		"/api/v1/users/"+url.PathEscape(argID),
		"user by ID",
	)
}

func (client *Client) userByPath(
	argContext context.Context,
	argAccessToken string,
	argPath string,
	argOperation string,
) (User, error) {
	var user User
	if err := client.doJSON(
		argContext,
		http.MethodGet,
		argPath,
		argAccessToken,
		nil,
		http.StatusOK,
		&user,
		argOperation,
	); err != nil {
		return User{}, err
	}

	if user.ID == "" || user.Username == "" {
		return User{}, fmt.Errorf("validate %s response: user is incomplete", argOperation)
	}

	return user, nil
}

// ListUsers returns one administrator-only user-directory page.
func (client *Client) ListUsers(
	argContext context.Context,
	argAccessToken string,
	argLimit int,
	argCursor string,
) (UserPage, error) {
	query := url.Values{}
	if argLimit != 0 {
		query.Set("limit", strconv.Itoa(argLimit))
	}
	if argCursor != "" {
		query.Set("cursor", argCursor)
	}

	path := "/api/v1/users"
	if encodedQuery := query.Encode(); encodedQuery != "" {
		path += "?" + encodedQuery
	}

	var page UserPage
	if err := client.doJSON(
		argContext,
		http.MethodGet,
		path,
		argAccessToken,
		nil,
		http.StatusOK,
		&page,
		"user directory",
	); err != nil {
		return UserPage{}, err
	}
	if page.Users == nil {
		return UserPage{}, errors.New(
			"validate user directory response: users are required",
		)
	}

	return page, nil
}

// CreateUser creates a credential-less user identity.
func (client *Client) CreateUser(
	argContext context.Context,
	argAccessToken string,
	argInput UserInput,
) (User, error) {
	return client.mutateUser(
		argContext,
		http.MethodPost,
		"/api/v1/users",
		argAccessToken,
		argInput,
		http.StatusCreated,
		"create user",
	)
}

// UpdateUser replaces mutable public details for an existing user.
func (client *Client) UpdateUser(
	argContext context.Context,
	argAccessToken string,
	argID string,
	argInput UserInput,
) (User, error) {
	return client.mutateUser(
		argContext,
		http.MethodPut,
		"/api/v1/users/"+url.PathEscape(argID),
		argAccessToken,
		argInput,
		http.StatusOK,
		"update user",
	)
}

func (client *Client) mutateUser(
	argContext context.Context,
	argMethod string,
	argPath string,
	argAccessToken string,
	argInput UserInput,
	argExpectedStatus int,
	argOperation string,
) (User, error) {
	var user User
	if err := client.doJSON(
		argContext,
		argMethod,
		argPath,
		argAccessToken,
		argInput,
		argExpectedStatus,
		&user,
		argOperation,
	); err != nil {
		return User{}, err
	}

	if user.ID == "" || user.Username == "" {
		return User{}, fmt.Errorf("validate %s response: user is incomplete", argOperation)
	}

	return user, nil
}

// SetUserActive activates or deactivates an existing user.
func (client *Client) SetUserActive(
	argContext context.Context,
	argAccessToken string,
	argID string,
	argActive bool,
) (User, error) {
	var user User
	if err := client.doJSON(
		argContext,
		http.MethodPut,
		"/api/v1/users/"+url.PathEscape(argID)+"/active",
		argAccessToken,
		struct {
			Active bool `json:"active"`
		}{Active: argActive},
		http.StatusOK,
		&user,
		"set user activation",
	); err != nil {
		return User{}, err
	}
	if user.ID == "" || user.Username == "" {
		return User{}, errors.New(
			"validate set user activation response: user is incomplete",
		)
	}

	return user, nil
}

// DeleteUser permanently removes a user identity and its authentication data.
func (client *Client) DeleteUser(
	argContext context.Context,
	argAccessToken string,
	argID string,
) error {
	return client.doJSON(
		argContext,
		http.MethodDelete,
		"/api/v1/users/"+url.PathEscape(argID),
		argAccessToken,
		nil,
		http.StatusNoContent,
		nil,
		"delete user",
	)
}
