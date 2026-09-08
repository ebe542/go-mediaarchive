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
func New(argBaseURL string, argHTTPClient *http.Client) *Client {
	if argHTTPClient == nil {
		argHTTPClient = &http.Client{Timeout: defaultTimeout}
	}

	return &Client{
		baseURL:    strings.TrimRight(argBaseURL, "/"),
		httpClient: argHTTPClient,
	}
}

// Health requests the current operational status from the server.
func (client *Client) Health(
	argContext context.Context,
) (HealthStatus, error) {
	var status HealthStatus
	if err := client.doJSON(
		argContext,
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
	argContext context.Context,
	argMethod string,
	argPath string,
	argAccessToken string,
	argRequestBody any,
	argExpectedStatus int,
	argResponseBody any,
	argOperation string,
) error {
	var requestBody io.Reader
	var encodedRequest []byte
	if argRequestBody != nil {
		var err error
		encodedRequest, err = json.Marshal(argRequestBody)
		if err != nil {
			return fmt.Errorf("encode %s request: %w", argOperation, err)
		}
		defer clearBytes(encodedRequest)
		requestBody = bytes.NewReader(encodedRequest)
	}

	request, err := http.NewRequestWithContext(
		argContext,
		argMethod,
		client.baseURL+argPath,
		requestBody,
	)
	if err != nil {
		return fmt.Errorf("create %s request: %w", argOperation, err)
	}
	request.Header.Set("Accept", "application/json")
	if argRequestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if argAccessToken != "" {
		request.Header.Set("Authorization", "Bearer "+argAccessToken)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", argOperation, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == argExpectedStatus && argResponseBody == nil {
		return nil
	}

	responseDocument, err := io.ReadAll(io.LimitReader(
		response.Body,
		maximumResponseBodySize+1,
	))
	if err != nil {
		return fmt.Errorf("read %s response: %w", argOperation, err)
	}
	if len(responseDocument) > maximumResponseBodySize {
		return fmt.Errorf("validate %s response: body is too large", argOperation)
	}

	if err := validateJSONContentType(response, argOperation); err != nil {
		return err
	}

	if response.StatusCode != argExpectedStatus {
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
				argOperation,
				response.Status,
			)
		}

		return &APIError{
			StatusCode: response.StatusCode,
			Code:       errorDocument.Error.Code,
			Message:    errorDocument.Error.Message,
		}
	}

	if err := decodeSingleJSON(responseDocument, argResponseBody); err != nil {
		return fmt.Errorf("decode %s response: %w", argOperation, err)
	}

	return nil
}

func validateJSONContentType(
	argResponse *http.Response,
	argOperation string,
) error {
	mediaType, _, err := mime.ParseMediaType(
		argResponse.Header.Get("Content-Type"),
	)
	if err != nil {
		return fmt.Errorf(
			"parse %s response Content-Type: %w",
			argOperation,
			err,
		)
	}
	if mediaType != "application/json" {
		return fmt.Errorf(
			"validate %s response Content-Type: expected %q, got %q",
			argOperation,
			"application/json",
			mediaType,
		)
	}

	return nil
}

func decodeSingleJSON(argDocument []byte, argDestination any) error {
	decoder := json.NewDecoder(bytes.NewReader(argDocument))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(argDestination); err != nil {
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

func clearBytes(argValue []byte) {
	for index := range argValue {
		argValue[index] = 0
	}
}
