package client

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

// Client communicates with the versioned Media Archive REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// HealthStatus describes the operational status returned by the server.
type HealthStatus struct {
	Status string `json:"status"`
}

// New creates an API client for the provided server base URL.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: defaultTimeout,
		}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// Health requests the current operational status from the server.
func (client *Client) Health(ctx context.Context) (HealthStatus, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		client.baseURL+"/api/v1/health",
		nil,
	)
	if err != nil {
		return HealthStatus{}, fmt.Errorf("create health request: %w", err)
	}

	request.Header.Set("Accept", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return HealthStatus{}, fmt.Errorf("request health status: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return HealthStatus{}, fmt.Errorf(
			"request health status: unexpected HTTP status %s",
			response.Status,
		)
	}
	mediaType, _, err := mime.ParseMediaType(
		response.Header.Get("Content-Type"),
	)
	if err != nil {
		return HealthStatus{}, fmt.Errorf(
			"parse health response Content-Type: %w",
			err,
		)
	}

	if mediaType != "application/json" {
		return HealthStatus{}, fmt.Errorf(
			"validate health response Content-Type: expected %q, got %q",
			"application/json",
			mediaType,
		)
	}
	var status HealthStatus
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		return HealthStatus{}, fmt.Errorf("decode health response: %w", err)
	}

	if strings.TrimSpace(status.Status) == "" {
		return HealthStatus{}, fmt.Errorf(
			"validate health response: status is required",
		)
	}
	return status, nil
}
