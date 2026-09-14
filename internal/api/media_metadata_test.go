package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/api"
	appmedia "github.com/ebe542/go-mediaarchive/internal/application/media"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	apiMediaID = "123e4567-e89b-12d3-a456-426614174000"
	apiOwnerID = "223e4567-e89b-12d3-a456-426614174000"
)

type recordingMediaMetadataService struct {
	item        domainmedia.Item
	err         error
	operation   string
	actor       identity.User
	id          string
	createInput appmedia.CreateItemInput
	updateInput appmedia.UpdateItemInput
	calls       int
}

func (service *recordingMediaMetadataService) CreateItem(
	_ context.Context,
	argActor identity.User,
	argInput appmedia.CreateItemInput,
) (domainmedia.Item, error) {
	service.record("create", argActor, "")
	service.createInput = argInput

	return service.item, service.err
}

func (service *recordingMediaMetadataService) ItemByID(
	_ context.Context,
	argActor identity.User,
	argID string,
) (domainmedia.Item, error) {
	service.record("read", argActor, argID)

	return service.item, service.err
}

func (service *recordingMediaMetadataService) UpdateItem(
	_ context.Context,
	argActor identity.User,
	argID string,
	argInput appmedia.UpdateItemInput,
) (domainmedia.Item, error) {
	service.record("update", argActor, argID)
	service.updateInput = argInput

	return service.item, service.err
}

func (service *recordingMediaMetadataService) DeleteItem(
	_ context.Context,
	argActor identity.User,
	argID string,
) error {
	service.record("delete", argActor, argID)

	return service.err
}

func (service *recordingMediaMetadataService) record(
	argOperation string,
	argActor identity.User,
	argID string,
) {
	service.calls++
	service.operation = argOperation
	service.actor = argActor
	service.id = argID
}

func TestMediaMetadataEndpointsRequireAuthentication(t *testing.T) {
	for _, testCase := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/media", validMediaRequestBody()},
		{http.MethodGet, "/api/v1/media/" + apiMediaID, ""},
		{http.MethodPut, "/api/v1/media/" + apiMediaID, validMediaRequestBody()},
		{http.MethodDelete, "/api/v1/media/" + apiMediaID, ""},
	} {
		service := &recordingMediaMetadataService{}
		handler := api.NewHandler(api.WithMediaMetadataAPI(
			&recordingSessionResolver{},
			service,
		))
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

func TestCreateMediaMetadataDecodesRequestAndReturnsResource(t *testing.T) {
	item := apiMediaItem(t, []string{"Example Author"})
	service := &recordingMediaMetadataService{item: item}
	resolver := mediaResolver()
	handler := api.NewHandler(api.WithMediaMetadataAPI(resolver, service))
	request := authenticatedMediaRequest(http.MethodPost, "/api/v1/media", validMediaRequestBody())
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Location") != "/api/v1/media/"+item.ID {
		t.Errorf("unexpected Location header %q", response.Header().Get("Location"))
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("expected no-store response, got %q", response.Header().Get("Cache-Control"))
	}
	if service.operation != "create" || service.actor.ID != resolver.user.ID {
		t.Fatalf("expected authenticated actor to reach create service, got %+v", service)
	}
	if service.createInput.Title != "Security Engineering" ||
		service.createInput.Type != domainmedia.TypeBook ||
		!bytes.Equal(service.createInput.Checksum, bytes.Repeat([]byte{0x5a}, sha256.Size)) {
		t.Fatalf("unexpected create input %+v", service.createInput)
	}
	assertMediaResponse(t, response, item)
}

func TestReadMediaMetadataReturnsEmptyAuthorsAsArray(t *testing.T) {
	item := apiMediaItem(t, nil)
	service := &recordingMediaMetadataService{item: item}
	resolver := mediaResolver()
	handler := api.NewHandler(api.WithMediaMetadataAPI(resolver, service))
	request := authenticatedMediaRequest(http.MethodGet, "/api/v1/media/"+item.ID, "")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.Code)
	}
	if service.operation != "read" || service.id != item.ID || service.actor.ID != resolver.user.ID {
		t.Fatalf("expected actor and media ID to reach read service, got %+v", service)
	}
	if !strings.Contains(response.Body.String(), `"authors":[]`) {
		t.Fatalf("expected empty authors array, got %s", response.Body.String())
	}
}

func TestUpdateAndDeleteMediaMetadataPassActorAndPathID(t *testing.T) {
	for _, testCase := range []struct {
		method    string
		body      string
		operation string
		status    int
	}{
		{http.MethodPut, validMediaRequestBody(), "update", http.StatusOK},
		{http.MethodDelete, "", "delete", http.StatusNoContent},
	} {
		item := apiMediaItem(t, []string{"Example Author"})
		service := &recordingMediaMetadataService{item: item}
		resolver := mediaResolver()
		handler := api.NewHandler(api.WithMediaMetadataAPI(resolver, service))
		request := authenticatedMediaRequest(testCase.method, "/api/v1/media/"+item.ID, testCase.body)
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)

		if response.Code != testCase.status {
			t.Errorf("%s: expected status %d, got %d", testCase.method, testCase.status, response.Code)
		}
		if service.operation != testCase.operation || service.id != item.ID || service.actor.ID != resolver.user.ID {
			t.Errorf("%s: unexpected service call %+v", testCase.method, service)
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: expected no-store response", testCase.method)
		}
	}
}

func TestMediaMetadataRejectsInvalidJSONAndChecksums(t *testing.T) {
	validChecksum := strings.Repeat("5a", sha256.Size)
	for name, testCase := range map[string]struct {
		contentType string
		body        string
	}{
		"missing content type": {"", validMediaRequestBody()},
		"unknown field": {"application/json", strings.Replace(
			validMediaRequestBody(), `}`, `,"storagePath":"secret"}`, 1,
		)},
		"uppercase checksum": {"application/json", strings.Replace(
			validMediaRequestBody(), validChecksum, strings.ToUpper(validChecksum), 1,
		)},
		"short checksum": {"application/json", strings.Replace(
			validMediaRequestBody(), validChecksum, "5a", 1,
		)},
		"non-hex checksum": {"application/json", strings.Replace(
			validMediaRequestBody(), validChecksum, strings.Repeat("zz", sha256.Size), 1,
		)},
		"trailing value": {"application/json", validMediaRequestBody() + `{}`},
	} {
		t.Run(name, func(t *testing.T) {
			service := &recordingMediaMetadataService{}
			handler := api.NewHandler(api.WithMediaMetadataAPI(mediaResolver(), service))
			request := httptest.NewRequest(http.MethodPost, "/api/v1/media", strings.NewReader(testCase.body))
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
				t.Fatal("expected invalid request not to reach media service")
			}
		})
	}
}

func TestMediaMetadataMapsApplicationErrors(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid input", domainmedia.ErrInvalidTitle, http.StatusBadRequest, "invalid_request"},
		{"creation forbidden", appmedia.ErrCreationForbidden, http.StatusForbidden, "forbidden"},
		{"hidden media", appmedia.ErrMediaNotFound, http.StatusNotFound, "not_found"},
		{"operational failure", errors.New("database unavailable"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			service := &recordingMediaMetadataService{err: testCase.err}
			handler := api.NewHandler(api.WithMediaMetadataAPI(mediaResolver(), service))
			request := authenticatedMediaRequest(http.MethodPost, "/api/v1/media", validMediaRequestBody())
			response := httptest.NewRecorder()

			handler.ServeHTTP(response, request)

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			if !strings.Contains(response.Body.String(), `"code":"`+testCase.code+`"`) {
				t.Fatalf("expected error code %q, got %s", testCase.code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), testCase.err.Error()) {
				t.Fatalf("expected internal error details to be hidden, got %s", response.Body.String())
			}
		})
	}
}

func authenticatedMediaRequest(argMethod string, argPath string, argBody string) *http.Request {
	request := httptest.NewRequest(argMethod, argPath, strings.NewReader(argBody))
	request.Header.Set("Authorization", "Bearer media-session")
	if argBody != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	return request
}

func mediaResolver() *recordingSessionResolver {
	return &recordingSessionResolver{user: identity.User{
		ID: apiOwnerID, Username: "media_owner", Role: identity.RoleEditor, Active: true,
	}}
}

func validMediaRequestBody() string {
	return `{"title":"Security Engineering","authors":["Example Author"],` +
		`"originalFilename":"security-engineering.pdf","type":"book",` +
		`"mimeType":"application/pdf","size":4096,"sha256":"` +
		strings.Repeat("5a", sha256.Size) + `"}`
}

func apiMediaItem(argTest *testing.T, argAuthors []string) domainmedia.Item {
	argTest.Helper()

	item, err := domainmedia.NewItem(
		apiMediaID,
		"Security Engineering",
		argAuthors,
		"security-engineering.pdf",
		domainmedia.TypeBook,
		"application/pdf",
		4096,
		bytes.Repeat([]byte{0x5a}, sha256.Size),
		apiOwnerID,
		time.Date(2026, time.September, 13, 10, 0, 0, 0, time.FixedZone("test", 2*60*60)),
		time.Date(2026, time.September, 13, 11, 0, 0, 0, time.FixedZone("test", 2*60*60)),
	)
	if err != nil {
		argTest.Fatalf("create media fixture: %v", err)
	}

	return item
}

func assertMediaResponse(
	argTest *testing.T,
	argResponse *httptest.ResponseRecorder,
	argItem domainmedia.Item,
) {
	argTest.Helper()

	var responseBody struct {
		ID        string    `json:"id"`
		Authors   []string  `json:"authors"`
		SHA256    string    `json:"sha256"`
		OwnerID   string    `json:"ownerId"`
		CreatedAt time.Time `json:"createdAt"`
	}
	if err := json.NewDecoder(argResponse.Body).Decode(&responseBody); err != nil {
		argTest.Fatalf("decode media response: %v", err)
	}
	if responseBody.ID != argItem.ID || responseBody.OwnerID != argItem.OwnerID ||
		responseBody.SHA256 != strings.Repeat("5a", sha256.Size) {
		argTest.Fatalf("unexpected media response %+v", responseBody)
	}
	if responseBody.CreatedAt.Location() != time.UTC {
		argTest.Fatalf("expected UTC timestamp, got %s", responseBody.CreatedAt)
	}
}
