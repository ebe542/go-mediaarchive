package api

import (
	"encoding/json"
	"net/http"
)

func NewHandler(argOptions ...Option) http.Handler {
	configuration := handlerConfiguration{}

	for _, option := range argOptions {
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
