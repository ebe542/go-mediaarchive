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

const clientGranteeID = "323e4567-e89b-12d3-a456-426614174000"

func TestMediaGrantOperationsUseTypedHTTPContract(t *testing.T) {
	permissions := clientPermissionSet(t, domainmedia.PermissionDiscover, domainmedia.PermissionRead)
	for _, testCase := range []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		hasRequestBody bool
		call           func(*client.Client) (client.MediaGrant, error)
	}{
		{
			"replace", http.MethodPut, clientGrantPath(), http.StatusOK, true,
			func(client *client.Client) (client.MediaGrant, error) {
				return client.ReplaceMediaGrant(
					context.Background(), "access-token", clientMediaID, clientGranteeID, permissions,
				)
			},
		},
		{
			"read", http.MethodGet, clientGrantPath(), http.StatusOK, false,
			func(client *client.Client) (client.MediaGrant, error) {
				return client.MediaGrantByUser(
					context.Background(), "access-token", clientMediaID, clientGranteeID,
				)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
				assertRequest(t, request, testCase.method, testCase.path, "access-token")
				if testCase.hasRequestBody {
					var body struct {
						Permissions []string `json:"permissions"`
					}
					decodeRequest(t, request, &body)
					if strings.Join(body.Permissions, ",") != "discover,read" {
						t.Errorf("unexpected permission request %v", body.Permissions)
					}
				}
				writeJSON(t, response, testCase.expectedStatus, map[string]any{
					"mediaId": clientMediaID, "userId": clientGranteeID,
					"permissions": []string{"discover", "read"},
				})
			})
			defer server.Close()

			grant, err := testCase.call(client.New(server.URL, server.Client()))
			if err != nil {
				t.Fatalf("%s media grant: %v", testCase.name, err)
			}
			if grant.MediaID != clientMediaID || grant.UserID != clientGranteeID ||
				grant.Permissions != permissions {
				t.Fatalf("unexpected media grant %+v", grant)
			}
		})
	}
}

func TestListAndRevokeMediaGrants(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		method string
		path   string
		status int
		call   func(*client.Client) error
	}{
		{
			"list", http.MethodGet, "/api/v1/media/" + clientMediaID + "/grants", http.StatusOK,
			func(client *client.Client) error {
				grants, err := client.ListMediaGrants(context.Background(), "access-token", clientMediaID)
				if err == nil && (len(grants) != 1 || !grants[0].Permissions.Has(domainmedia.PermissionRead)) {
					return errors.New("unexpected media grants")
				}

				return err
			},
		},
		{
			"revoke", http.MethodDelete, clientGrantPath(), http.StatusNoContent,
			func(client *client.Client) error {
				return client.RevokeMediaGrant(
					context.Background(), "access-token", clientMediaID, clientGranteeID,
				)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, request *http.Request) {
				assertRequest(t, request, testCase.method, testCase.path, "access-token")
				if testCase.name == "list" {
					writeJSON(t, response, http.StatusOK, map[string]any{"grants": []map[string]any{{
						"mediaId": clientMediaID, "userId": clientGranteeID,
						"permissions": []string{"read"},
					}}})

					return
				}
				response.WriteHeader(http.StatusNoContent)
			})
			defer server.Close()

			if err := testCase.call(client.New(server.URL, server.Client())); err != nil {
				t.Fatalf("%s media grants: %v", testCase.name, err)
			}
		})
	}
}

func TestListMediaGrantsAcceptsEmptyArrayAndRejectsMissingOrNull(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		body      string
		expectErr bool
	}{
		{"empty", `{"grants":[]}`, false},
		{"missing", `{}`, true},
		{"null", `{"grants":null}`, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				response.WriteHeader(http.StatusOK)
				_, _ = response.Write([]byte(testCase.body))
			})
			defer server.Close()

			grants, err := client.New(server.URL, server.Client()).ListMediaGrants(
				context.Background(), "access-token", clientMediaID,
			)
			if testCase.expectErr && err == nil {
				t.Fatal("expected invalid grant list response to be rejected")
			}
			if !testCase.expectErr && (err != nil || grants == nil || len(grants) != 0) {
				t.Fatalf("expected an empty grant list, got %v, %v", grants, err)
			}
		})
	}
}

func TestMediaGrantClientRejectsInvalidPermissions(t *testing.T) {
	apiClient := client.New("http://127.0.0.1", nil)
	if _, err := apiClient.ReplaceMediaGrant(
		context.Background(), "access-token", clientMediaID, clientGranteeID, 0,
	); !errors.Is(err, domainmedia.ErrInvalidPermissionSet) {
		t.Fatalf("expected ErrInvalidPermissionSet, got %v", err)
	}

	for name, permissions := range map[string]string{
		"missing":   "",
		"null":      `null`,
		"empty":     `[]`,
		"duplicate": `["read","read"]`,
		"unknown":   `["stream"]`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"mediaId":"` + clientMediaID + `","userId":"` + clientGranteeID + `"`
			if permissions != "" {
				body += `,"permissions":` + permissions
			}
			body += `}`
			server := newJSONServer(t, func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Content-Type", "application/json")
				response.WriteHeader(http.StatusOK)
				_, _ = response.Write([]byte(body))
			})
			defer server.Close()

			_, err := client.New(server.URL, server.Client()).MediaGrantByUser(
				context.Background(), "access-token", clientMediaID, clientGranteeID,
			)
			if err == nil {
				t.Fatal("expected invalid permission response to be rejected")
			}
		})
	}
}

func clientGrantPath() string {
	return "/api/v1/media/" + clientMediaID + "/grants/" + clientGranteeID
}

func clientPermissionSet(
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
