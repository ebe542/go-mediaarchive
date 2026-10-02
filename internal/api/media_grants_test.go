package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const apiGranteeID = "323e4567-e89b-12d3-a456-426614174000"

type recordingMediaGrantService struct {
	grant       domainmedia.Grant
	grants      []domainmedia.Grant
	err         error
	operation   string
	actor       identity.User
	mediaID     string
	userID      string
	permissions domainmedia.PermissionSet
	calls       int
}

func (service *recordingMediaGrantService) ReplaceGrant(
	_ context.Context,
	actor identity.User,
	mediaID string,
	userID string,
	permissions domainmedia.PermissionSet,
) (domainmedia.Grant, error) {
	service.record("replace", actor, mediaID, userID)
	service.permissions = permissions

	return service.grant, service.err
}

func (service *recordingMediaGrantService) GrantByUser(
	_ context.Context,
	actor identity.User,
	mediaID string,
	userID string,
) (domainmedia.Grant, error) {
	service.record("read", actor, mediaID, userID)

	return service.grant, service.err
}

func (service *recordingMediaGrantService) GrantsByMedia(
	_ context.Context,
	actor identity.User,
	mediaID string,
) ([]domainmedia.Grant, error) {
	service.record("list", actor, mediaID, "")

	return service.grants, service.err
}

func (service *recordingMediaGrantService) RevokeGrant(
	_ context.Context,
	actor identity.User,
	mediaID string,
	userID string,
) error {
	service.record("revoke", actor, mediaID, userID)

	return service.err
}

func (service *recordingMediaGrantService) record(
	operation string,
	actor identity.User,
	mediaID string,
	userID string,
) {
	service.calls++
	service.operation = operation
	service.actor = actor
	service.mediaID = mediaID
	service.userID = userID
}

func TestMediaGrantEndpointsRequireAuthentication(t *testing.T) {
	for _, testCase := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPut, grantPath(), `{"permissions":["read"]}`},
		{http.MethodGet, grantPath(), ""},
		{http.MethodGet, "/api/v1/media/" + apiMediaID + "/grants", ""},
		{http.MethodDelete, grantPath(), ""},
	} {
		service := &recordingMediaGrantService{}
		handler := api.NewHandler(api.WithMediaGrantAPI(&recordingSessionResolver{}, service))
		request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
		if testCase.body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: expected status 401, got %d", testCase.method, testCase.path, response.Code)
		}
		if service.calls != 0 {
			t.Errorf("%s %s: expected service not to be called", testCase.method, testCase.path)
		}
	}
}

func TestReplaceMediaGrantParsesPermissionsAndReturnsStableNames(t *testing.T) {
	permissions := grantPermissionSet(t, domainmedia.PermissionDiscover, domainmedia.PermissionRead)
	grant := grantFixture(t, permissions)
	service := &recordingMediaGrantService{grant: grant}
	resolver := mediaResolver()
	handler := api.NewHandler(api.WithMediaGrantAPI(resolver, service))
	request := authenticatedGrantRequest(
		http.MethodPut,
		grantPath(),
		`{"permissions":["read","discover"]}`,
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.operation != "replace" || service.actor.ID != resolver.user.ID ||
		service.mediaID != apiMediaID || service.userID != apiGranteeID {
		t.Fatalf("unexpected replacement call %+v", service)
	}
	if service.permissions != permissions {
		t.Fatalf("expected permission set %d, got %d", permissions, service.permissions)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("expected no-store response, got %q", response.Header().Get("Cache-Control"))
	}
	if !strings.Contains(response.Body.String(), `"permissions":["discover","read"]`) {
		t.Fatalf("expected stable permission order, got %s", response.Body.String())
	}
}

func TestReadListAndRevokeMediaGrantsPassResourceIdentifiers(t *testing.T) {
	grant := grantFixture(t, grantPermissionSet(t, domainmedia.PermissionRead))
	for _, testCase := range []struct {
		method    string
		path      string
		operation string
		status    int
	}{
		{http.MethodGet, grantPath(), "read", http.StatusOK},
		{http.MethodGet, "/api/v1/media/" + apiMediaID + "/grants", "list", http.StatusOK},
		{http.MethodDelete, grantPath(), "revoke", http.StatusNoContent},
	} {
		service := &recordingMediaGrantService{grant: grant, grants: []domainmedia.Grant{grant}}
		resolver := mediaResolver()
		handler := api.NewHandler(api.WithMediaGrantAPI(resolver, service))
		request := authenticatedGrantRequest(testCase.method, testCase.path, "")
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != testCase.status {
			t.Errorf("%s: expected status %d, got %d", testCase.operation, testCase.status, response.Code)
		}
		if service.operation != testCase.operation || service.actor.ID != resolver.user.ID ||
			service.mediaID != apiMediaID {
			t.Errorf("%s: unexpected service call %+v", testCase.operation, service)
		}
		if testCase.operation != "list" && service.userID != apiGranteeID {
			t.Errorf("%s: expected grantee ID, got %+v", testCase.operation, service)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: expected no-store response", testCase.operation)
		}
	}
}

func TestListMediaGrantsEncodesEmptyArray(t *testing.T) {
	service := &recordingMediaGrantService{grants: nil}
	handler := api.NewHandler(api.WithMediaGrantAPI(mediaResolver(), service))
	request := authenticatedGrantRequest(
		http.MethodGet,
		"/api/v1/media/"+apiMediaID+"/grants",
		"",
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"grants":[]`) {
		t.Fatalf("expected empty grants array, got %s", response.Body.String())
	}
}

func TestReplaceMediaGrantRejectsInvalidRequestsBeforeServiceCall(t *testing.T) {
	for name, testCase := range map[string]struct {
		contentType string
		body        string
	}{
		"missing content type": {"", `{"permissions":["read"]}`},
		"missing permissions":  {"application/json", `{}`},
		"null permissions":     {"application/json", `{"permissions":null}`},
		"empty permissions":    {"application/json", `{"permissions":[]}`},
		"duplicate permission": {"application/json", `{"permissions":["read","read"]}`},
		"unknown permission":   {"application/json", `{"permissions":["stream"]}`},
		"uppercase permission": {"application/json", `{"permissions":["Read"]}`},
		"unknown field":        {"application/json", `{"permissions":["read"],"role":"admin"}`},
	} {
		t.Run(name, func(t *testing.T) {
			service := &recordingMediaGrantService{}
			handler := api.NewHandler(api.WithMediaGrantAPI(mediaResolver(), service))
			request := httptest.NewRequest(http.MethodPut, grantPath(), strings.NewReader(testCase.body))
			request.Header.Set("Authorization", "Bearer media-session")
			if testCase.contentType != "" {
				request.Header.Set("Content-Type", testCase.contentType)
			}
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", response.Code)
			}
			if service.calls != 0 {
				t.Fatal("expected invalid request not to reach grant service")
			}
		})
	}
}

func TestMediaGrantMapsApplicationErrors(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid user ID", identity.ErrInvalidUserID, http.StatusBadRequest, "invalid_request"},
		{"hidden media", appmedia.ErrMediaNotFound, http.StatusNotFound, "not_found"},
		{"missing grant", domainmedia.ErrGrantNotFound, http.StatusNotFound, "not_found"},
		{"missing user", identity.ErrUserNotFound, http.StatusNotFound, "not_found"},
		{"owner grant", appmedia.ErrOwnerGrant, http.StatusConflict, "owner_grant"},
		{"inactive grantee", appmedia.ErrInactiveGrantee, http.StatusConflict, "inactive_grantee"},
		{"operational failure", errors.New("database unavailable"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service := &recordingMediaGrantService{err: testCase.err}
			handler := api.NewHandler(api.WithMediaGrantAPI(mediaResolver(), service))
			request := authenticatedGrantRequest(http.MethodGet, grantPath(), "")
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			if !strings.Contains(response.Body.String(), `"code":"`+testCase.code+`"`) {
				t.Fatalf("expected error code %q, got %s", testCase.code, response.Body.String())
			}
			if testCase.status == http.StatusInternalServerError &&
				strings.Contains(response.Body.String(), testCase.err.Error()) {
				t.Fatalf("expected internal error details to be hidden, got %s", response.Body.String())
			}
		})
	}
}

func authenticatedGrantRequest(method string, path string, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer media-session")
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	return request
}

func grantPath() string {
	return "/api/v1/media/" + apiMediaID + "/grants/" + apiGranteeID
}

func grantFixture(
	test *testing.T,
	permissions domainmedia.PermissionSet,
) domainmedia.Grant {
	test.Helper()

	grant, err := domainmedia.NewGrant(apiMediaID, apiGranteeID, permissions)
	if err != nil {
		test.Fatalf("create grant fixture: %v", err)
	}

	return grant
}

func grantPermissionSet(
	test *testing.T,
	requested ...domainmedia.Permission,
) domainmedia.PermissionSet {
	test.Helper()

	permissionSet, err := domainmedia.NewPermissionSet(requested...)
	if err != nil {
		test.Fatalf("create permission set: %v", err)
	}

	return permissionSet
}
