package api

import (
	"encoding/json"
	"net/http"

	"github.com/ebe542/go-mediaarchive/internal/identity"
)

func NewHandler(options ...Option) http.Handler {
	configuration := handlerConfiguration{}

	for _, option := range options {
		option(&configuration)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", handleHealth)

	if configuration.sessions != nil &&
		configuration.limiter != nil &&
		configuration.clock != nil {
		authentication := &authenticationHandler{
			sessions: configuration.sessions,
			limiter:  configuration.limiter,
			clock:    configuration.clock,
		}

		mux.HandleFunc(
			"POST /api/v1/auth/sessions",
			authentication.createSession,
		)
		mux.HandleFunc(
			"DELETE /api/v1/auth/sessions/current",
			authentication.revokeCurrentSession,
		)
	}

	if configuration.sessionResolver != nil &&
		configuration.userReader != nil {
		users := &userReadHandler{
			users: configuration.userReader,
		}

		currentUserHandler := RequireAuthentication(
			configuration.sessionResolver,
			RequireRoles(
				http.HandlerFunc(users.currentUser),
				identity.RoleViewer,
				identity.RoleEditor,
				identity.RoleAdmin,
			),
		)

		mux.Handle(
			"GET /api/v1/users/me",
			currentUserHandler,
		)

		userByIDHandler := RequireAuthentication(
			configuration.sessionResolver,
			RequireRoles(
				http.HandlerFunc(users.userByID),
				identity.RoleAdmin,
			),
		)

		mux.Handle(
			"GET /api/v1/users/{id}",
			userByIDHandler,
		)
	}

	if configuration.sessionResolver != nil &&
		configuration.userWriter != nil {
		users := &userWriteHandler{
			users: configuration.userWriter,
		}

		administratorOnly := func(handler http.Handler) http.Handler {
			return RequireAuthentication(
				configuration.sessionResolver,
				RequireRoles(handler, identity.RoleAdmin),
			)
		}

		mux.Handle(
			"POST /api/v1/users",
			administratorOnly(http.HandlerFunc(users.createUser)),
		)
		mux.Handle(
			"PUT /api/v1/users/{id}",
			administratorOnly(http.HandlerFunc(users.updateUser)),
		)
		mux.Handle(
			"PUT /api/v1/users/{id}/active",
			administratorOnly(http.HandlerFunc(users.setUserActive)),
		)
		mux.Handle(
			"DELETE /api/v1/users/{id}",
			administratorOnly(http.HandlerFunc(users.deleteUser)),
		)
	}

	if configuration.passwordEnrollmentResolver != nil &&
		configuration.passwordEnrollments != nil &&
		configuration.passwordEnrollmentLimiter != nil &&
		configuration.passwordEnrollmentClock != nil {
		passwordEnrollments := &passwordEnrollmentHandler{
			service: configuration.passwordEnrollments,
			limiter: configuration.passwordEnrollmentLimiter,
			clock:   configuration.passwordEnrollmentClock,
		}

		issueEnrollment := RequireAuthentication(
			configuration.passwordEnrollmentResolver,
			RequireRoles(
				http.HandlerFunc(passwordEnrollments.issue),
				identity.RoleAdmin,
			),
		)

		mux.Handle(
			"POST /api/v1/users/{id}/password-enrollment",
			issueEnrollment,
		)
		mux.HandleFunc(
			"POST /api/v1/auth/password-enrollments",
			passwordEnrollments.complete,
		)
	}

	if configuration.passwordChangeResolver != nil &&
		configuration.passwordChanges != nil {
		passwordChanges := &passwordChangeHandler{
			service: configuration.passwordChanges,
		}

		changePassword := RequireAuthentication(
			configuration.passwordChangeResolver,
			RequireRoles(
				http.HandlerFunc(
					passwordChanges.changeCurrentUserPassword,
				),
				identity.RoleViewer,
				identity.RoleEditor,
				identity.RoleAdmin,
			),
		)

		mux.Handle(
			"PUT /api/v1/users/me/password",
			changePassword,
		)
	}

	if configuration.userDirectoryResolver != nil &&
		configuration.userLister != nil {
		directory := &userDirectoryHandler{
			users: configuration.userLister,
		}

		listUsers := RequireAuthentication(
			configuration.userDirectoryResolver,
			RequireRoles(
				http.HandlerFunc(directory.listUsers),
				identity.RoleAdmin,
			),
		)

		mux.Handle("GET /api/v1/users", listUsers)
	}

	if configuration.mediaResolver != nil &&
		configuration.mediaMetadata != nil {
		mediaHandler := &mediaMetadataHandler{media: configuration.mediaMetadata}
		authenticated := func(handler http.Handler) http.Handler {
			return RequireAuthentication(configuration.mediaResolver, handler)
		}

		mux.Handle(
			"POST /api/v1/media",
			authenticated(http.HandlerFunc(mediaHandler.create)),
		)
		mux.Handle(
			"GET /api/v1/media/{id}",
			authenticated(http.HandlerFunc(mediaHandler.read)),
		)
		mux.Handle(
			"PUT /api/v1/media/{id}",
			authenticated(http.HandlerFunc(mediaHandler.update)),
		)
		mux.Handle(
			"DELETE /api/v1/media/{id}",
			authenticated(http.HandlerFunc(mediaHandler.delete)),
		)
	}

	if configuration.mediaUploadResolver != nil &&
		configuration.mediaUploads != nil &&
		configuration.maximumUploadSize > 0 {
		uploadHandler := &mediaUploadHandler{
			uploads:     configuration.mediaUploads,
			maximumSize: configuration.maximumUploadSize,
		}
		mux.Handle(
			"POST /api/v1/media/uploads",
			RequireAuthentication(
				configuration.mediaUploadResolver,
				http.HandlerFunc(uploadHandler.upload),
			),
		)
	}

	if configuration.mediaContentResolver != nil &&
		configuration.mediaContent != nil {
		contentHandler := &mediaContentHandler{
			content: configuration.mediaContent,
		}
		authenticated := RequireAuthentication(
			configuration.mediaContentResolver,
			http.HandlerFunc(contentHandler.serve),
		)
		mux.Handle("GET /api/v1/media/{id}/content", authenticated)
		mux.Handle("HEAD /api/v1/media/{id}/content", authenticated)
	}

	if configuration.mediaGrantResolver != nil &&
		configuration.mediaGrants != nil {
		grantHandler := &mediaGrantHandler{grants: configuration.mediaGrants}
		authenticated := func(handler http.Handler) http.Handler {
			return RequireAuthentication(configuration.mediaGrantResolver, handler)
		}

		mux.Handle(
			"PUT /api/v1/media/{id}/grants/{userId}",
			authenticated(http.HandlerFunc(grantHandler.replace)),
		)
		mux.Handle(
			"GET /api/v1/media/{id}/grants/{userId}",
			authenticated(http.HandlerFunc(grantHandler.read)),
		)
		mux.Handle(
			"GET /api/v1/media/{id}/grants",
			authenticated(http.HandlerFunc(grantHandler.list)),
		)
		mux.Handle(
			"DELETE /api/v1/media/{id}/grants/{userId}",
			authenticated(http.HandlerFunc(grantHandler.revoke)),
		)
	}

	return mux
}

func handleHealth(response http.ResponseWriter, _ *http.Request) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(http.StatusOK)

	_ = json.NewEncoder(response).Encode(struct {
		Status string `json:"status"`
	}{
		Status: "ok",
	})
}
