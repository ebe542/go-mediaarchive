package client_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/client"
)

func TestIssuePasswordEnrollment(t *testing.T) {
	expiresAt := time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(
			t,
			request,
			http.MethodPost,
			"/api/v1/users/user-id/password-enrollment",
			"access-token",
		)
		writeJSON(t, response, http.StatusCreated, map[string]any{
			"token":     "one-time-token",
			"expiresAt": expiresAt,
		})
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	enrollment, err := apiClient.IssuePasswordEnrollment(
		context.Background(),
		"access-token",
		"user-id",
	)
	if err != nil {
		t.Fatalf("issue password enrollment: %v", err)
	}
	if enrollment.Token != "one-time-token" || !enrollment.ExpiresAt.Equal(expiresAt) {
		t.Errorf("unexpected enrollment: %+v", enrollment)
	}
}

func TestCompletePasswordEnrollment(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodPost, "/api/v1/auth/password-enrollments", "")
		var body struct {
			Token    string `json:"token"`
			Password string `json:"password"`
		}
		decodeRequest(t, request, &body)
		if body.Token != "one-time-token" || body.Password != "synthetic passphrase" {
			t.Errorf("unexpected enrollment request: %+v", body)
		}
		response.WriteHeader(http.StatusNoContent)
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	err := apiClient.CompletePasswordEnrollment(
		context.Background(),
		"one-time-token",
		[]byte("synthetic passphrase"),
	)
	if err != nil {
		t.Fatalf("complete password enrollment: %v", err)
	}
}

func TestChangePassword(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodPut, "/api/v1/users/me/password", "access-token")
		var body struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword     string `json:"newPassword"`
		}
		decodeRequest(t, request, &body)
		if body.CurrentPassword != "current passphrase" ||
			body.NewPassword != "new passphrase" {
			t.Errorf("unexpected password change request: %+v", body)
		}
		response.WriteHeader(http.StatusNoContent)
	})
	defer server.Close()

	apiClient := client.New(server.URL, server.Client())
	err := apiClient.ChangePassword(
		context.Background(),
		"access-token",
		[]byte("current passphrase"),
		[]byte("new passphrase"),
	)
	if err != nil {
		t.Fatalf("change password: %v", err)
	}
}
