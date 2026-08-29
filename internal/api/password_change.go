package api

import (
	"context"
	"errors"
	"net/http"

	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// PasswordChangeService verifies and replaces an authenticated user's password.
type PasswordChangeService interface {
	ChangePassword(
		argContext context.Context,
		argUserID string,
		argCurrentPassword []byte,
		argNewPassword []byte,
	) error
}

// WithPasswordChangeAPI enables authenticated self-service password changes.
func WithPasswordChangeAPI(
	argResolver SessionResolver,
	argService PasswordChangeService,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.passwordChangeResolver = argResolver
		argConfiguration.passwordChanges = argService
	}
}

type passwordChangeHandler struct {
	service PasswordChangeService
}

func (handler *passwordChangeHandler) changeCurrentUserPassword(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	user, exists := AuthenticatedUser(argRequest.Context())
	if !exists {
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	var requestBody struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSONRequest(
		argResponse,
		argRequest,
		&requestBody,
	); err != nil ||
		requestBody.CurrentPassword == "" ||
		requestBody.NewPassword == "" {
		writeInvalidRequest(argResponse)

		return
	}

	currentPassword := []byte(requestBody.CurrentPassword)
	newPassword := []byte(requestBody.NewPassword)
	defer clearBytes(currentPassword)
	defer clearBytes(newPassword)

	err := handler.service.ChangePassword(
		argRequest.Context(),
		user.ID,
		currentPassword,
		newPassword,
	)
	if errors.Is(err, apppasswords.ErrInvalidCurrentPassword) {
		writeJSONError(
			argResponse,
			http.StatusUnauthorized,
			"invalid_credentials",
			"Invalid current password.",
		)

		return
	}
	if errors.Is(err, password.ErrInvalidPassword) ||
		errors.Is(err, apppasswords.ErrPasswordUnchanged) {
		writeInvalidRequest(argResponse)

		return
	}
	if err != nil {
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusNoContent)
}
