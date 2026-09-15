package client_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
	"testing"

	"github.com/ebe542/go-mediaarchive/internal/client"
	domainmedia "github.com/ebe542/go-mediaarchive/internal/media"
)

const (
	clientMediaID = "123e4567-e89b-12d3-a456-426614174000"
	clientOwnerID = "223e4567-e89b-12d3-a456-426614174000"
)

func TestMediaMetadataOperationsUseTypedHTTPContract(t *testing.T) {
	input := clientMediaInput()
	for _, testCase := range []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		hasRequestBody bool
		call           func(*client.Client) (client.Media, error)
	}{
		{
			"create", http.MethodPost, "/api/v1/media", http.StatusCreated, true,
			func(argClient *client.Client) (client.Media, error) {
				return argClient.CreateMedia(context.Background(), "access-token", input)
			},
		},
		{
			"read", http.MethodGet, "/api/v1/media/" + clientMediaID, http.StatusOK, false,
			func(argClient *client.Client) (client.Media, error) {
				return argClient.MediaByID(context.Background(), "access-token", clientMediaID)
			},
		},
		{
			"update", http.MethodPut, "/api/v1/media/" + clientMediaID, http.StatusOK, true,
			func(argClient *client.Client) (client.Media, error) {
				return argClient.UpdateMedia(context.Background(), "access-token", clientMediaID, input)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
				assertRequest(t, request, testCase.method, testCase.path, "access-token")
				if testCase.hasRequestBody {
					var body struct {
						Title   string   `json:"title"`
						Authors []string `json:"authors"`
						SHA256  string   `json:"sha256"`
					}
					decodeRequest(t, request, &body)
					if body.Title != input.Title || len(body.Authors) != 1 ||
						body.SHA256 != strings.Repeat("5a", sha256.Size) {
						t.Errorf("unexpected media request %+v", body)
					}
				}
				writeClientMediaResponse(t, response, testCase.expectedStatus)
			})
			defer server.Close()

			item, err := testCase.call(client.New(server.URL, server.Client()))
			if err != nil {
				t.Fatalf("%s media: %v", testCase.name, err)
			}
			if item.ID != clientMediaID || item.OwnerID != clientOwnerID ||
				item.Checksum != input.Checksum {
				t.Fatalf("unexpected media response %+v", item)
			}
		})
	}
}

func TestDeleteMediaUsesAuthenticatedResourcePath(t *testing.T) {
	server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
		assertRequest(t, request, http.MethodDelete, "/api/v1/media/"+clientMediaID, "access-token")
		response.WriteHeader(http.StatusNoContent)
	})
	defer server.Close()

	if err := client.New(server.URL, server.Client()).DeleteMedia(
		context.Background(),
		"access-token",
		clientMediaID,
	); err != nil {
		t.Fatalf("delete media: %v", err)
	}
}

func TestMediaClientRejectsInvalidResponses(t *testing.T) {
	valid := clientMediaResponseJSON()
	for name, responseBody := range map[string]string{
		"missing authors":    strings.Replace(valid, `"authors":["Example Author"],`, "", 1),
		"null authors":       strings.Replace(valid, `"authors":["Example Author"]`, `"authors":null`, 1),
		"uppercase checksum": strings.Replace(valid, strings.Repeat("5a", sha256.Size), strings.Repeat("5A", sha256.Size), 1),
		"short checksum":     strings.Replace(valid, strings.Repeat("5a", sha256.Size), "5a", 1),
		"invalid media ID":   strings.Replace(valid, clientMediaID, "media-id", 1),
		"zero timestamp":     strings.Replace(valid, `"2026-09-14T08:00:00Z"`, `"0001-01-01T00:00:00Z"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				response.WriteHeader(http.StatusOK)
				_, _ = response.Write([]byte(responseBody))
			})
			defer server.Close()

			_, err := client.New(server.URL, server.Client()).MediaByID(
				context.Background(),
				"access-token",
				clientMediaID,
			)
			if err == nil {
				t.Fatal("expected invalid media response to be rejected")
			}
		})
	}
}

func clientMediaInput() client.MediaInput {
	var checksum [sha256.Size]byte
	copy(checksum[:], bytes.Repeat([]byte{0x5a}, sha256.Size))

	return client.MediaInput{
		Title:            "Security Engineering",
		Authors:          []string{"Example Author"},
		OriginalFilename: "security-engineering.pdf",
		Type:             domainmedia.TypeBook,
		MIMEType:         "application/pdf",
		Size:             4096,
		Checksum:         checksum,
	}
}

func writeClientMediaResponse(
	argTest *testing.T,
	argResponse http.ResponseWriter,
	argStatus int,
) {
	argTest.Helper()

	argResponse.Header().Set("Content-Type", "application/json")
	argResponse.WriteHeader(argStatus)
	_, _ = argResponse.Write([]byte(clientMediaResponseJSON()))
}

func clientMediaResponseJSON() string {
	return `{"id":"` + clientMediaID + `","title":"Security Engineering",` +
		`"authors":["Example Author"],"originalFilename":"security-engineering.pdf",` +
		`"type":"book","mimeType":"application/pdf","size":4096,"sha256":"` +
		strings.Repeat("5a", sha256.Size) + `","ownerId":"` + clientOwnerID + `",` +
		`"createdAt":"2026-09-14T08:00:00Z","updatedAt":"2026-09-14T09:00:00Z"}`
}
