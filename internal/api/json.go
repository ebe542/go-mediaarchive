package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
)

const maximumJSONBodySize = 64 * 1024

func decodeJSONRequest(
	argResponse http.ResponseWriter,
	argRequest *http.Request,
	argDestination any,
) error {
	mediaType, _, err := mime.ParseMediaType(
		argRequest.Header.Get("Content-Type"),
	)
	if err != nil || mediaType != "application/json" {
		return errors.New("expected application/json content type")
	}

	argRequest.Body = http.MaxBytesReader(
		argResponse,
		argRequest.Body,
		maximumJSONBodySize,
	)

	decoder := json.NewDecoder(argRequest.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(argDestination); err != nil {
		return fmt.Errorf("decode JSON request: %w", err)
	}

	if err := ensureJSONEnd(decoder); err != nil {
		return err
	}

	return nil
}

func ensureJSONEnd(argDecoder *json.Decoder) error {
	var additionalValue any

	if err := argDecoder.Decode(&additionalValue); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("additional JSON value")
		}

		return fmt.Errorf("decode trailing JSON: %w", err)
	}

	return nil
}
