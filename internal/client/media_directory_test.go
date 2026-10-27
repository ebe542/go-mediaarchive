package client_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/client"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const secondClientMediaID = "023e4567-e89b-12d3-a456-426614174000"

func TestListMediaUsesFiltersAndDecodesOrderedPage(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodGet, "/api/v1/media", "access-token")
		query := request.URL.Query()
		if query.Get("query") != "Security & Go" ||
			query.Get("type") != string(domainmedia.TypeBook) ||
			query.Get("limit") != "2" ||
			query.Get("cursor") != "opaque_cursor" {
			t.Fatalf("unexpected media query: %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(mediaDirectoryPageJSON()))
	})
	defer server.Close()

	page, err := client.New(server.URL, server.Client()).ListMedia(
		context.Background(),
		"access-token",
		client.MediaDirectoryFilter{
			Query:  "  Security & Go  ",
			Type:   domainmedia.TypeBook,
			Limit:  2,
			Cursor: "opaque_cursor",
		},
	)
	if err != nil {
		t.Fatalf("list media: %v", err)
	}
	if len(page.Items) != 2 || page.Items[0].ID != clientMediaID ||
		page.Items[1].ID != secondClientMediaID || page.NextCursor != "next_cursor" {
		t.Fatalf("unexpected media page: %+v", page)
	}
	if page.Items[0].Checksum != clientMediaInput().Checksum {
		t.Fatalf("unexpected media checksum: %x", page.Items[0].Checksum)
	}
}

func TestListMediaAcceptsEmptyFinalPage(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodGet, "/api/v1/media", "access-token")
		if request.URL.RawQuery != "" {
			t.Fatalf("expected no media query, got %q", request.URL.RawQuery)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"items":[]}`))
	})
	defer server.Close()

	page, err := client.New(server.URL, server.Client()).ListMedia(
		context.Background(), "access-token", client.MediaDirectoryFilter{},
	)
	if err != nil {
		t.Fatalf("list empty media page: %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatalf("unexpected empty media page: %+v", page)
	}
}

func TestListMediaRejectsInvalidFiltersBeforeRequest(t *testing.T) {
	for name, filter := range map[string]client.MediaDirectoryFilter{
		"long query":     {Query: strings.Repeat("x", 201)},
		"control query":  {Query: "unsafe\nquery"},
		"media type":     {Type: "audio"},
		"negative limit": {Limit: -1},
		"large limit":    {Limit: 101},
		"cursor space":   {Cursor: "unsafe cursor"},
		"cursor size":    {Cursor: strings.Repeat("a", 1025)},
	} {
		t.Run(name, func(t *testing.T) {
			server := newJSONServer(t, func(http.ResponseWriter, *http.Request) {
				t.Fatal("expected invalid filter not to send a request")
			})
			defer server.Close()

			_, err := client.New(server.URL, server.Client()).ListMedia(
				context.Background(), "access-token", filter,
			)
			if err == nil {
				t.Fatal("expected invalid media filter error")
			}
		})
	}
}

func TestListMediaRejectsInvalidResponses(t *testing.T) {
	valid := mediaDirectoryPageJSON()
	secondItem := directoryMediaJSON(
		secondClientMediaID,
		"2026-09-13T08:00:00Z",
		"2026-09-13T09:00:00Z",
	)
	for name, responseBody := range map[string]string{
		"missing items":  `{}`,
		"null items":     `{"items":null}`,
		"too many items": valid,
		"duplicate ID": `{"items":[` + directoryMediaJSON(
			clientMediaID, "2026-09-14T08:00:00Z", "2026-09-14T09:00:00Z",
		) + `,` + directoryMediaJSON(
			clientMediaID, "2026-09-13T08:00:00Z", "2026-09-13T09:00:00Z",
		) + `]}`,
		"ascending order": `{"items":[` + secondItem + `,` + directoryMediaJSON(
			clientMediaID, "2026-09-14T08:00:00Z", "2026-09-14T09:00:00Z",
		) + `]}`,
		"ID tie-break order": `{"items":[` + directoryMediaJSON(
			secondClientMediaID, "2026-09-14T08:00:00Z", "2026-09-14T09:00:00Z",
		) + `,` + directoryMediaJSON(
			clientMediaID, "2026-09-14T08:00:00Z", "2026-09-14T09:00:00Z",
		) + `]}`,
		"unsafe cursor":       strings.Replace(valid, "next_cursor", "unsafe cursor", 1),
		"partial cursor page": `{"items":[` + secondItem + `],"nextCursor":"next_cursor"}`,
		"non-UTC timestamp": strings.Replace(
			valid, "2026-09-14T08:00:00Z", "2026-09-14T10:00:00+02:00", 1,
		),
		"unknown field": strings.Replace(valid, `"nextCursor"`, `"unknown":true,"nextCursor"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				_, _ = response.Write([]byte(responseBody))
			})
			defer server.Close()

			limit := 2
			if name == "too many items" {
				limit = 1
			}
			_, err := client.New(server.URL, server.Client()).ListMedia(
				context.Background(),
				"access-token",
				client.MediaDirectoryFilter{Limit: limit},
			)
			if err == nil {
				t.Fatal("expected invalid media directory response")
			}
		})
	}
}

func TestListMediaReturnsSafeAPIError(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte(
			`{"error":{"code":"forbidden","message":"Access forbidden."}}`,
		))
	})
	defer server.Close()

	_, err := client.New(server.URL, server.Client()).ListMedia(
		context.Background(), "access-token", client.MediaDirectoryFilter{},
	)
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden ||
		apiErr.Code != "forbidden" {
		t.Fatalf("expected safe forbidden API error, got %v", err)
	}
}

func mediaDirectoryPageJSON() string {
	return `{"items":[` + directoryMediaJSON(
		clientMediaID,
		"2026-09-14T08:00:00Z",
		"2026-09-14T09:00:00Z",
	) + `,` + directoryMediaJSON(
		secondClientMediaID,
		"2026-09-13T08:00:00Z",
		"2026-09-13T09:00:00Z",
	) + `],"nextCursor":"next_cursor"}`
}

func directoryMediaJSON(id string, createdAt string, updatedAt string) string {
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(clientMediaResponseJSON(), clientMediaID, id),
			"2026-09-14T08:00:00Z",
			createdAt,
		),
		"2026-09-14T09:00:00Z",
		updatedAt,
	)
}
