package client_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/client"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

func TestUserReadOperations(t *testing.T) {
	tests := []struct {
		name string
		path string
		call func(*client.Client) (client.User, error)
	}{
		{
			name: "current user",
			path: "/api/v1/users/me",
			call: func(argClient *client.Client) (client.User, error) {
				return argClient.CurrentUser(context.Background(), "access-token")
			},
		},
		{
			name: "user by ID",
			path: "/api/v1/users/user-id",
			call: func(argClient *client.Client) (client.User, error) {
				return argClient.UserByID(context.Background(), "access-token", "user-id")
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
				assertRequest(t, request, http.MethodGet, testCase.path, "access-token")
				writeUser(t, response, http.StatusOK, true)
			})
			defer server.Close()

			user, err := testCase.call(client.New(server.URL, server.Client()))
			if err != nil {
				t.Fatalf("read user: %v", err)
			}
			assertUser(t, user, true)
		})
	}
}

func TestListUsersUsesPaginationQuery(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodGet, "/api/v1/users", "access-token")
		if request.URL.Query().Get("limit") != "25" ||
			request.URL.Query().Get("cursor") != "opaque cursor" {
			t.Errorf("unexpected pagination query: %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte(`{"users":[],"nextCursor":"next-cursor"}`))
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	page, err := apiClient.ListUsers(
		context.Background(),
		"access-token",
		25,
		"opaque cursor",
	)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if page.Users == nil || page.NextCursor != "next-cursor" {
		t.Errorf("unexpected user page: %+v", page)
	}
}

func TestUserWriteOperations(t *testing.T) {
	input := client.UserInput{
		Username:    "archive_editor",
		DisplayName: "Archive Editor",
		Role:        identity.RoleEditor,
	}
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		call           func(*client.Client) (client.User, error)
	}{
		{
			name:           "create",
			method:         http.MethodPost,
			path:           "/api/v1/users",
			expectedStatus: http.StatusCreated,
			call: func(argClient *client.Client) (client.User, error) {
				return argClient.CreateUser(context.Background(), "access-token", input)
			},
		},
		{
			name:           "update",
			method:         http.MethodPut,
			path:           "/api/v1/users/user-id",
			expectedStatus: http.StatusOK,
			call: func(argClient *client.Client) (client.User, error) {
				return argClient.UpdateUser(context.Background(), "access-token", "user-id", input)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
				assertRequest(t, request, testCase.method, testCase.path, "access-token")
				var body client.UserInput
				decodeRequest(t, request, &body)
				if body != input {
					t.Errorf("unexpected user input: %+v", body)
				}
				writeUser(t, response, testCase.expectedStatus, true)
			})
			defer server.Close()

			user, err := testCase.call(client.New(server.URL, server.Client()))
			if err != nil {
				t.Fatalf("write user: %v", err)
			}
			assertUser(t, user, true)
		})
	}
}

func TestSetUserActive(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodPut, "/api/v1/users/user-id/active", "access-token")
		var body struct {
			Active bool `json:"active"`
		}
		decodeRequest(t, request, &body)
		if body.Active {
			t.Error("expected deactivation request")
		}
		writeUser(t, response, http.StatusOK, false)
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	user, err := apiClient.SetUserActive(
		context.Background(),
		"access-token",
		"user-id",
		false,
	)
	if err != nil {
		t.Fatalf("deactivate user: %v", err)
	}
	assertUser(t, user, false)
}

func writeUser(
	argTest *testing.T,
	argResponse http.ResponseWriter,
	argStatus int,
	argActive bool,
) {
	argTest.Helper()

	writeJSON(argTest, argResponse, argStatus, client.User{
		ID:          "user-id",
		Username:    "archive_editor",
		DisplayName: "Archive Editor",
		Role:        identity.RoleEditor,
		Active:      argActive,
		CreatedAt:   time.Date(2026, time.September, 8, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, time.September, 8, 11, 0, 0, 0, time.UTC),
	})
}

func assertUser(argTest *testing.T, argUser client.User, argActive bool) {
	argTest.Helper()

	if argUser.ID != "user-id" ||
		argUser.Username != "archive_editor" ||
		argUser.Active != argActive {
		argTest.Errorf("unexpected user: %+v", argUser)
	}
}
