package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	appsessions "github.com/ebe542/go-mediaarchive/internal/application/sessions"
	"github.com/ebe542/go-mediaarchive/internal/identity"
)

// SessionResolver resolves an authenticated user from an opaque access token.
type SessionResolver interface {
	Resolve(
		ctx context.Context,
		accessToken string,
	) (identity.User, error)
}

type authenticatedUserContextKey struct{}

// AuthenticatedUser returns the authenticated user stored in a request context.
func AuthenticatedUser(
	ctx context.Context,
) (identity.User, bool) {
	user, exists := ctx.Value(
		authenticatedUserContextKey{},
	).(identity.User)

	return user, exists
}

// RequireAuthentication resolves a bearer token before calling a protected handler.
func RequireAuthentication(
	resolver SessionResolver,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		accessToken, err := bearerToken(
			request.Header.Values("Authorization"),
		)
		if err != nil {
			writeAuthenticationRequired(response)

			return
		}

		user, err := resolver.Resolve(
			request.Context(),
			accessToken,
		)
		if errors.Is(err, appsessions.ErrUnauthenticated) {
			writeAuthenticationRequired(response)

			return
		}
		if err != nil {
			writeJSONError(
				response,
				http.StatusInternalServerError,
				"internal_error",
				"Internal server error.",
			)

			return
		}
		requestContext := context.WithValue(
			request.Context(),
			authenticatedUserContextKey{},
			user,
		)

		next.ServeHTTP(
			response,
			request.WithContext(requestContext),
		)
	})
}

func writeAuthenticationRequired(
	response http.ResponseWriter,
) {
	response.Header().Set(
		"WWW-Authenticate",
		"Bearer",
	)
	writeJSONError(
		response,
		http.StatusUnauthorized,
		"authentication_required",
		"Authentication required.",
	)
}

// RequireRoles permits a request when its authenticated user has an allowed role.
func RequireRoles(
	next http.Handler,
	allowedRoles ...identity.Role,
) http.Handler {
	return http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		user, exists := AuthenticatedUser(request.Context())
		if !exists {
			writeAuthenticationRequired(response)

			return
		}

		if slices.Contains(allowedRoles, user.Role) {
			next.ServeHTTP(response, request)

			return
		}

		writeJSONError(
			response,
			http.StatusForbidden,
			"forbidden",
			"Access forbidden.",
		)
	})
}
