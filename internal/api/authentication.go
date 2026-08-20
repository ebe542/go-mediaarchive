package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/application/authentication"
	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
)

const maximumAuthenticationBodySize = 64 * 1024

// SessionService creates and revokes authenticated sessions.
type SessionService interface {
	Create(
		argContext context.Context,
		argUsername string,
		argPassword []byte,
	) (appsessions.Created, error)

	Revoke(
		argContext context.Context,
		argAccessToken string,
	) error
}

// AttemptLimiter limits failed login attempts.
type AttemptLimiter interface {
	Allow(
		argUsername string,
		argSourceIP string,
		argNow time.Time,
	) bool

	RecordFailure(
		argUsername string,
		argSourceIP string,
		argNow time.Time,
	)

	RecordSuccess(
		argUsername string,
		argSourceIP string,
	)

	Cancel(
		argUsername string,
		argSourceIP string,
	)
}

// Clock returns the current API time.
type Clock func() time.Time

type handlerConfiguration struct {
	sessions SessionService
	limiter  AttemptLimiter
	clock    Clock
}

// Option configures optional API capabilities.
type Option func(argConfiguration *handlerConfiguration)

// WithAuthentication enables the authentication session endpoints.
func WithAuthentication(
	argSessions SessionService,
	argLimiter AttemptLimiter,
	argClock Clock,
) Option {
	return func(argConfiguration *handlerConfiguration) {
		argConfiguration.sessions = argSessions
		argConfiguration.limiter = argLimiter
		argConfiguration.clock = argClock
	}
}

type authenticationHandler struct {
	sessions SessionService
	limiter  AttemptLimiter
	clock    Clock
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (handler *authenticationHandler) createSession(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	mediaType, _, err := mime.ParseMediaType(
		argRequest.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != "application/json" {
		writeJSONError(
			argResponse,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	argRequest.Body = http.MaxBytesReader(
		argResponse,
		argRequest.Body,
		maximumAuthenticationBodySize,
	)

	var requestBody struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	decoder := json.NewDecoder(argRequest.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&requestBody); err != nil {
		writeJSONError(
			argResponse,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	if err := ensureJSONEnd(decoder); err != nil {
		writeJSONError(
			argResponse,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	if requestBody.Username == "" ||
		requestBody.Password == "" {
		writeJSONError(
			argResponse,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	sourceIP, err := sourceIPAddress(argRequest.RemoteAddr)
	if err != nil {
		writeJSONError(
			argResponse,
			http.StatusBadRequest,
			"invalid_request",
			"Invalid request.",
		)

		return
	}

	currentTime := handler.clock().UTC()

	if !handler.limiter.Allow(
		requestBody.Username,
		sourceIP,
		currentTime,
	) {
		writeJSONError(
			argResponse,
			http.StatusTooManyRequests,
			"too_many_requests",
			"Too many authentication attempts.",
		)

		return
	}

	passwordBytes := []byte(requestBody.Password)
	defer clearBytes(passwordBytes)

	createdSession, err := handler.sessions.Create(
		argRequest.Context(),
		requestBody.Username,
		passwordBytes,
	)
	if errors.Is(err, authentication.ErrInvalidCredentials) {
		handler.limiter.RecordFailure(
			requestBody.Username,
			sourceIP,
			currentTime,
		)

		writeJSONError(
			argResponse,
			http.StatusUnauthorized,
			"invalid_credentials",
			"Invalid username or password.",
		)

		return
	}
	if err != nil {
		handler.limiter.Cancel(
			requestBody.Username,
			sourceIP,
		)

		writeJSONError(
			argResponse,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
		)

		return
	}

	handler.limiter.RecordSuccess(requestBody.Username, sourceIP)

	argResponse.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(argResponse).Encode(struct {
		AccessToken string    `json:"accessToken"`
		TokenType   string    `json:"tokenType"`
		ExpiresAt   time.Time `json:"expiresAt"`
	}{
		AccessToken: createdSession.AccessToken,
		TokenType:   "Bearer",
		ExpiresAt:   createdSession.ExpiresAt,
	})
}

func ensureJSONEnd(argDecoder *json.Decoder) error {
	var additionalValue any

	if err := argDecoder.Decode(&additionalValue); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("additional JSON value")
		}

		return fmt.Errorf("decode trailing JSON: %w", err)
	}

	return nil
}

func sourceIPAddress(argRemoteAddress string) (string, error) {
	host, _, err := net.SplitHostPort(argRemoteAddress)
	if err != nil {
		return "", fmt.Errorf("parse remote address: %w", err)
	}

	return host, nil
}

func clearBytes(argValue []byte) {
	for index := range argValue {
		argValue[index] = 0
	}

	// Keep the slice alive until clearing has completed.
	runtime.KeepAlive(argValue)
}

func writeJSONError(
	argResponse http.ResponseWriter,
	argStatus int,
	argCode string,
	argMessage string,
) {
	responseBody := errorResponse{}
	responseBody.Error.Code = argCode
	responseBody.Error.Message = argMessage

	argResponse.Header().Set(
		"Content-Type",
		"application/json; charset=utf-8",
	)
	argResponse.Header().Set("Cache-Control", "no-store")
	argResponse.WriteHeader(argStatus)

	_ = json.NewEncoder(argResponse).Encode(responseBody)
}

func (handler *authenticationHandler) revokeCurrentSession(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
) {
	accessToken, err := bearerToken(
		argRequest.Header.Values("Authorization"),
	)
	if err != nil {
		writeJSONError(
			argResponse,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication required.",
		)

		return
	}

	if err := handler.sessions.Revoke(
		argRequest.Context(),
		accessToken,
	); err != nil {
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

func bearerToken(argAuthorizationHeaders []string) (string, error) {
	if len(argAuthorizationHeaders) != 1 {
		return "", errors.New(
			"expected exactly one Authorization header",
		)
	}

	parts := strings.Fields(argAuthorizationHeaders[0])
	if len(parts) != 2 ||
		!strings.EqualFold(parts[0], "Bearer") ||
		parts[1] == "" {
		return "", errors.New("expected a bearer token")
	}

	return parts[1], nil
}
