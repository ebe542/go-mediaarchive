// Package client provides a typed client for the Media Archive REST API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
)

const (
	defaultTimeout          = 10 * time.Second
	maximumResponseBodySize = 1024 * 1024
)

// Client communicates with the versioned Media Archive REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// APIError is a safe structured error returned by the REST API.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

// Error returns the stable API code and safe public message.
func (err *APIError) Error() string {
	return fmt.Sprintf("API error %s: %s", err.Code, err.Message)
}

// HealthStatus describes the operational status returned by the server.
type HealthStatus struct {
	Status string `json:"status"`
}

// New creates an API client for the provided server base URL.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// Health requests the current operational status from the server.
func (client *Client) Health(
	ctx context.Context,
) (HealthStatus, error) {
	var status HealthStatus
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		"/api/v1/health",
		"",
		nil,
		http.StatusOK,
		&status,
		"health",
	); err != nil {
		return HealthStatus{}, err
	}

	if strings.TrimSpace(status.Status) == "" {
		return HealthStatus{}, errors.New(
			"validate health response: status is required",
		)
	}

	return status, nil
}

func (client *Client) doJSON(
	ctx context.Context,
	method string,
	path string,
	accessToken string,
	requestDocument any,
	expectedStatus int,
	responseBody any,
	operation string,
) error {
	var requestBody io.Reader
	var encodedRequest []byte
	if requestDocument != nil {
		var err error
		encodedRequest, err = json.Marshal(requestDocument)
		if err != nil {
			return fmt.Errorf("encode %s request: %w", operation, err)
		}
		defer clearBytes(encodedRequest)
		requestBody = bytes.NewReader(encodedRequest)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		method,
		client.baseURL+path,
		requestBody,
	)
	if err != nil {
		return fmt.Errorf("create %s request: %w", operation, err)
	}
	request.Header.Set("Accept", "application/json")
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		request.Header.Set("Authorization", "Bearer "+accessToken)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", operation, err)
	}
	defer func() { _ = response.Body.Close() }()

	return handleJSONResponse(
		response,
		expectedStatus,
		responseBody,
		operation,
	)
}

func handleJSONResponse(
	response *http.Response,
	expectedStatus int,
	responseBody any,
	operation string,
) error {
	if response.StatusCode == expectedStatus && responseBody == nil {
		return nil
	}

	responseDocument, err := io.ReadAll(io.LimitReader(
		response.Body,
		maximumResponseBodySize+1,
	))
	if err != nil {
		return fmt.Errorf("read %s response: %w", operation, err)
	}
	if len(responseDocument) > maximumResponseBodySize {
		return fmt.Errorf("validate %s response: body is too large", operation)
	}

	if err := validateJSONContentType(response, operation); err != nil {
		return err
	}

	if response.StatusCode != expectedStatus {
		var errorDocument struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := decodeSingleJSON(responseDocument, &errorDocument); err != nil ||
			errorDocument.Error.Code == "" ||
			errorDocument.Error.Message == "" {
			return fmt.Errorf(
				"request %s: unexpected HTTP status %s",
				operation,
				response.Status,
			)
		}

		return &APIError{
			StatusCode: response.StatusCode,
			Code:       errorDocument.Error.Code,
			Message:    errorDocument.Error.Message,
		}
	}

	if err := decodeSingleJSON(responseDocument, responseBody); err != nil {
		return fmt.Errorf("decode %s response: %w", operation, err)
	}

	return nil
}

func validateJSONContentType(
	response *http.Response,
	operation string,
) error {
	mediaType, _, err := mime.ParseMediaType(
		response.Header.Get("Content-Type"),
	)
	if err != nil {
		return fmt.Errorf(
			"parse %s response Content-Type: %w",
			operation,
			err,
		)
	}
	if mediaType != "application/json" {
		return fmt.Errorf(
			"validate %s response Content-Type: expected %q, got %q",
			operation,
			"application/json",
			mediaType,
		)
	}

	return nil
}

func decodeSingleJSON(document []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	var additionalValue any
	if err := decoder.Decode(&additionalValue); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("additional JSON value")
		}

		return err
	}

	return nil
}

func clearBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
