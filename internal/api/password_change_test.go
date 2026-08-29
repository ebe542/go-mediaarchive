package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/api"
	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

type recordingPasswordChangeService struct {
	userID          string
	currentPassword []byte
	newPassword     []byte
	err             error
}

func (service *recordingPasswordChangeService) ChangePassword(
	_ context.Context,
	argUserID string,
	argCurrentPassword []byte,
	argNewPassword []byte,
) error {
	service.userID = argUserID
	service.currentPassword = append([]byte(nil), argCurrentPassword...)
	service.newPassword = append([]byte(nil), argNewPassword...)

	return service.err
}

func TestPasswordChangeUsesAuthenticatedUserAndExactPasswords(t *testing.T) {
	service := &recordingPasswordChangeService{}
	handler := passwordChangeTestHandler(service)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/users/me/password",
		strings.NewReader(
			`{"currentPassword":"current synthetic passphrase","newPassword":"new synthetic passphrase"}`,
		),
	)
	request.Header.Set("Authorization", "Bearer session-token")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf(
			"expected status %d, got %d: %s",
			http.StatusNoContent,
			response.Code,
			response.Body.String(),
		)
	}
	if service.userID != "123e4567-e89b-12d3-a456-426614174000" ||
		string(service.currentPassword) != "current synthetic passphrase" ||
		string(service.newPassword) != "new synthetic passphrase" {
		t.Fatalf("unexpected password change input: %+v", service)
	}
}

func TestPasswordChangeMapsApplicationErrors(t *testing.T) {
	testCases := []struct {
		name           string
		err            error
		expectedStatus int
		expectedCode   string
	}{
		{
			"invalid current password",
			apppasswords.ErrInvalidCurrentPassword,
			http.StatusUnauthorized,
			"invalid_credentials",
		},
		{
			"invalid new password",
			password.ErrInvalidPassword,
			http.StatusBadRequest,
			"invalid_request",
		},
		{
			"unchanged password",
			apppasswords.ErrPasswordUnchanged,
			http.StatusBadRequest,
			"invalid_request",
		},
		{
			"unexpected error",
			errors.New("database unavailable"),
			http.StatusInternalServerError,
			"internal_error",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := passwordChangeTestHandler(
				&recordingPasswordChangeService{err: testCase.err},
			)
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/v1/users/me/password",
				strings.NewReader(
					`{"currentPassword":"current synthetic passphrase","newPassword":"new synthetic passphrase"}`,
				),
			)
			request.Header.Set("Authorization", "Bearer session-token")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			assertPasswordEnrollmentError(
				t,
				response,
				testCase.expectedStatus,
				testCase.expectedCode,
			)
		})
	}
}

func TestPasswordChangeRequiresAuthentication(t *testing.T) {
	handler := passwordChangeTestHandler(&recordingPasswordChangeService{})
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/users/me/password",
		strings.NewReader(`{}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	assertPasswordEnrollmentError(
		t,
		response,
		http.StatusUnauthorized,
		"authentication_required",
	)
}

func passwordChangeTestHandler(
	argService api.PasswordChangeService,
) http.Handler {
	return api.NewHandler(
		api.WithPasswordChangeAPI(
			passwordEnrollmentSessionResolver{
				user: identity.User{
					ID:   "123e4567-e89b-12d3-a456-426614174000",
					Role: identity.RoleViewer,
				},
			},
			argService,
		),
	)
}
