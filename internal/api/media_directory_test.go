package api_test

import (
	"context"
	"encoding/json"
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

type recordingMediaDirectoryLister struct {
	actor identity.User
	input appmedia.ListItemsInput
	page  appmedia.ItemPage
	err   error
	calls int
}

func (lister *recordingMediaDirectoryLister) ListItems(
	_ context.Context,
	actor identity.User,
	input appmedia.ListItemsInput,
) (appmedia.ItemPage, error) {
	lister.actor = actor
	lister.input = input
	lister.calls++

	return lister.page, lister.err
}

func TestMediaDirectoryRequiresAuthentication(t *testing.T) {
	lister := &recordingMediaDirectoryLister{}
	handler := api.NewHandler(api.WithMediaDirectoryAPI(
		&recordingSessionResolver{},
		lister,
	))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/media", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", response.Code)
	}
	if lister.calls != 0 {
		t.Fatal("expected unauthenticated request not to reach media directory")
	}
}

func TestMediaDirectoryReturnsFilteredPageAndOpaqueCursor(t *testing.T) {
	item := apiMediaItem(t, []string{"Example Author"})
	nextCursor := appmedia.DirectoryCursor{
		CreatedAt:  item.CreatedAt,
		ID:         item.ID,
		FilterHash: [32]byte{1},
		Limit:      25,
	}
	lister := &recordingMediaDirectoryLister{page: appmedia.ItemPage{
		Items:      []domainmedia.Item{item},
		NextCursor: &nextCursor,
	}}
	handler := api.NewHandler(api.WithMediaDirectoryAPI(mediaResolver(), lister))
	request := authenticatedMediaRequest(
		http.MethodGet,
		"/api/v1/media?query=Security&type=book&limit=25",
		"",
	)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("expected no-store, got %q", response.Header().Get("Cache-Control"))
	}
	if lister.actor.ID != apiOwnerID || lister.input.Filter.Query != "Security" ||
		lister.input.Filter.Type != domainmedia.TypeBook || lister.input.Limit != 25 {
		t.Fatalf("unexpected directory input: %+v", lister)
	}
	var body struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"nextCursor"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode media directory response: %v", err)
	}
	if len(body.Items) != 1 || body.Items[0].ID != item.ID || body.NextCursor == "" {
		t.Fatalf("unexpected media directory response: %+v", body)
	}
	if strings.Contains(body.NextCursor, item.ID) || strings.Contains(body.NextCursor, "Security") {
		t.Fatalf("expected opaque cursor, got %q", body.NextCursor)
	}

	lister.page = appmedia.ItemPage{Items: []domainmedia.Item{}}
	nextRequest := authenticatedMediaRequest(
		http.MethodGet,
		"/api/v1/media?query=Security&type=book&limit=25&cursor="+body.NextCursor,
		"",
	)
	nextResponse := httptest.NewRecorder()
	handler.ServeHTTP(nextResponse, nextRequest)

	if nextResponse.Code != http.StatusOK {
		t.Fatalf("expected cursor status 200, got %d: %s", nextResponse.Code, nextResponse.Body.String())
	}
	if lister.input.Cursor == nil || lister.input.Cursor.ID != item.ID ||
		lister.input.Cursor.FilterHash != nextCursor.FilterHash ||
		lister.input.Cursor.Limit != nextCursor.Limit {
		t.Fatalf("unexpected decoded cursor: %+v", lister.input.Cursor)
	}
}

func TestMediaDirectoryRejectsInvalidQueries(t *testing.T) {
	for _, path := range []string{
		"/api/v1/media?unknown=value",
		"/api/v1/media?query=",
		"/api/v1/media?query=one&query=two",
		"/api/v1/media?limit=0",
		"/api/v1/media?limit=101",
		"/api/v1/media?cursor=not-base64",
		"/api/v1/media?cursor=" + strings.Repeat("a", 1025),
	} {
		t.Run(path, func(t *testing.T) {
			lister := &recordingMediaDirectoryLister{}
			handler := api.NewHandler(api.WithMediaDirectoryAPI(mediaResolver(), lister))
			response := httptest.NewRecorder()

			handler.ServeHTTP(
				response,
				authenticatedMediaRequest(http.MethodGet, path, ""),
			)

			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", response.Code)
			}
			if lister.calls != 0 {
				t.Fatal("expected invalid query not to reach media directory")
			}
		})
	}
}

func TestMediaDirectoryMapsApplicationErrors(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid filter", appmedia.ErrInvalidDirectoryFilter, http.StatusBadRequest, "invalid_request"},
		{"invalid cursor", appmedia.ErrInvalidDirectoryCursor, http.StatusBadRequest, "invalid_request"},
		{"inactive actor", appmedia.ErrDirectoryForbidden, http.StatusForbidden, "forbidden"},
		{"operational failure", errors.New("database unavailable"), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			lister := &recordingMediaDirectoryLister{err: testCase.err}
			handler := api.NewHandler(api.WithMediaDirectoryAPI(mediaResolver(), lister))
			response := httptest.NewRecorder()

			handler.ServeHTTP(
				response,
				authenticatedMediaRequest(http.MethodGet, "/api/v1/media", ""),
			)

			if response.Code != testCase.status {
				t.Fatalf("expected status %d, got %d", testCase.status, response.Code)
			}
			if !strings.Contains(response.Body.String(), `"code":"`+testCase.code+`"`) {
				t.Fatalf("expected error code %q, got %s", testCase.code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), testCase.err.Error()) {
				t.Fatalf("expected internal details to be hidden, got %s", response.Body.String())
			}
		})
	}
}

func TestMediaDirectoryReturnsAnEmptyJSONArray(t *testing.T) {
	lister := &recordingMediaDirectoryLister{page: appmedia.ItemPage{
		Items: []domainmedia.Item{},
	}}
	handler := api.NewHandler(api.WithMediaDirectoryAPI(mediaResolver(), lister))
	response := httptest.NewRecorder()

	handler.ServeHTTP(
		response,
		authenticatedMediaRequest(http.MethodGet, "/api/v1/media", ""),
	)

	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("expected an empty media array, got %d: %s", response.Code, response.Body.String())
	}
}
