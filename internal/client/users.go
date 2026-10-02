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
	ctx context.Context,
	accessToken string,
) (User, error) {
	return client.userByPath(
		ctx,
		accessToken,
		"/api/v1/users/me",
		"current user",
	)
}

// UserByID returns a user identity by stable ID.
func (client *Client) UserByID(
	ctx context.Context,
	accessToken string,
	id string,
) (User, error) {
	return client.userByPath(
		ctx,
		accessToken,
		"/api/v1/users/"+url.PathEscape(id),
		"user by ID",
	)
}

func (client *Client) userByPath(
	ctx context.Context,
	accessToken string,
	path string,
	operation string,
) (User, error) {
	var user User
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		path,
		accessToken,
		nil,
		http.StatusOK,
		&user,
		operation,
	); err != nil {
		return User{}, err
	}

	if user.ID == "" || user.Username == "" {
		return User{}, fmt.Errorf("validate %s response: user is incomplete", operation)
	}

	return user, nil
}

// ListUsers returns one administrator-only user-directory page.
func (client *Client) ListUsers(
	ctx context.Context,
	accessToken string,
	limit int,
	cursor string,
) (UserPage, error) {
	query := url.Values{}
	if limit != 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}

	path := "/api/v1/users"
	if encodedQuery := query.Encode(); encodedQuery != "" {
		path += "?" + encodedQuery
	}

	var page UserPage
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		path,
		accessToken,
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
	ctx context.Context,
	accessToken string,
	input UserInput,
) (User, error) {
	return client.mutateUser(
		ctx,
		http.MethodPost,
		"/api/v1/users",
		accessToken,
		input,
		http.StatusCreated,
		"create user",
	)
}

// UpdateUser replaces mutable public details for an existing user.
func (client *Client) UpdateUser(
	ctx context.Context,
	accessToken string,
	id string,
	input UserInput,
) (User, error) {
	return client.mutateUser(
		ctx,
		http.MethodPut,
		"/api/v1/users/"+url.PathEscape(id),
		accessToken,
		input,
		http.StatusOK,
		"update user",
	)
}

func (client *Client) mutateUser(
	ctx context.Context,
	method string,
	path string,
	accessToken string,
	input UserInput,
	expectedStatus int,
	operation string,
) (User, error) {
	var user User
	if err := client.doJSON(
		ctx,
		method,
		path,
		accessToken,
		input,
		expectedStatus,
		&user,
		operation,
	); err != nil {
		return User{}, err
	}

	if user.ID == "" || user.Username == "" {
		return User{}, fmt.Errorf("validate %s response: user is incomplete", operation)
	}

	return user, nil
}

// SetUserActive activates or deactivates an existing user.
func (client *Client) SetUserActive(
	ctx context.Context,
	accessToken string,
	id string,
	active bool,
) (User, error) {
	var user User
	if err := client.doJSON(
		ctx,
		http.MethodPut,
		"/api/v1/users/"+url.PathEscape(id)+"/active",
		accessToken,
		struct {
			Active bool `json:"active"`
		}{Active: active},
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
	ctx context.Context,
	accessToken string,
	id string,
) error {
	return client.doJSON(
		ctx,
		http.MethodDelete,
		"/api/v1/users/"+url.PathEscape(id),
		accessToken,
		nil,
		http.StatusNoContent,
		nil,
		"delete user",
	)
}
