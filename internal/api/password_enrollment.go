package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	apppasswords "github.com/ebe542/go-mediaarchive/internal/application/passwords"
	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	"github.com/ebe542/go-mediaarchive/internal/password"
)

// PasswordEnrollmentService issues tokens and creates initial credentials.
type PasswordEnrollmentService interface {
	IssueEnrollment(
		argContext context.Context,
		argUserID string,
	) (apppasswords.IssuedEnrollment, error)

	CompleteEnrollment(
		argContext context.Context,
		argToken string,
		argPassword []byte,
	) error
}

// PasswordEnrollmentAttemptLimiter limits public attempts by source IP.
type PasswordEnrollmentAttemptLimiter interface {
	Allow(argSourceIP string, argNow time.Time) bool
	RecordFailure(argSourceIP string, argNow time.Time)
	RecordSuccess(argSourceIP string)
	Cancel(argSourceIP string)
}

// WithPasswordEnrollmentAPI enables password enrollment HTTP endpoints.
func WithPasswordEnrollmentAPI(
	argResolver SessionResolver,
	argService PasswordEnrollmentService,
	argLimiter PasswordEnrollmentAttemptLimiter,
	argClock Clock,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.passwordEnrollmentResolver = argResolver
		argConfiguration.passwordEnrollments = argService
		argConfiguration.passwordEnrollmentLimiter = argLimiter
		argConfiguration.passwordEnrollmentClock = argClock
	}
}

type passwordEnrollmentHandler struct {
	service PasswordEnrollmentService
	limiter PasswordEnrollmentAttemptLimiter
	clock   Clock
}

func (handler *passwordEnrollmentHandler) issue(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	issued, err := handler.service.IssueEnrollment(
		argRequest.Context(),
		argRequest.PathValue("id"),
	)
	if err != nil {
		writePasswordEnrollmentIssueError(argResponse, err)

		return
	}

	argResponse.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(argResponse).Encode(struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expiresAt"`
	}{
		Token:     issued.Token,
		ExpiresAt: issued.ExpiresAt,
	})
}

func (handler *passwordEnrollmentHandler) complete(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	var requestBody struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := decodeJSONRequest(
		argResponse,
		argRequest,
		&requestBody,
	); err != nil || requestBody.Token == "" || requestBody.Password == "" {
		writeInvalidRequest(argResponse)

		return
	}

	sourceIP, err := sourceIPAddress(argRequest.RemoteAddr)
	if err != nil {
		writeInvalidRequest(argResponse)

		return
	}

	currentTime := handler.clock().UTC()
	if !handler.limiter.Allow(sourceIP, currentTime) {
		writeJSONError(
			argResponse,
			http.StatusTooManyRequests,
			"too_many_requests",
			"Too many password enrollment attempts.",
		)

		return
	}

	passwordBytes := []byte(requestBody.Password)
	defer clearBytes(passwordBytes)

	err = handler.service.CompleteEnrollment(
		argRequest.Context(),
		requestBody.Token,
		passwordBytes,
	)
	if errors.Is(err, apppasswords.ErrInvalidEnrollment) {
		handler.limiter.RecordFailure(sourceIP, currentTime)
		writeJSONError(
			argResponse,
			http.StatusUnauthorized,
			"invalid_enrollment",
			"Invalid password enrollment.",
		)

		return
	}
	if errors.Is(err, password.ErrInvalidPassword) {
		handler.limiter.Cancel(sourceIP)
		writeInvalidRequest(argResponse)

		return
	}
	if err != nil {
		handler.limiter.Cancel(sourceIP)
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	handler.limiter.RecordSuccess(sourceIP)
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusNoContent)
}

func writePasswordEnrollmentIssueError(
	argResponse http.ResponseWriter,
	argError error,
) {
	switch {
	case errors.Is(argError, identity.ErrInvalidUserID):
		writeInvalidRequest(argResponse)
	case errors.Is(argError, identity.ErrUserNotFound):
		writeJSONError(
			argResponse,
			http.StatusNotFound,
			"not_found",
			"Resource not found.",
		)
	case errors.Is(argError, credential.ErrPasswordCredentialExists):
		writeJSONError(
			argResponse,
			http.StatusConflict,
			"credential_exists",
			"A password credential already exists.",
		)
	default:
		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)
	}
}
