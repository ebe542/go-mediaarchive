package api

import (
	"errors"
	"strconv"
	"strings"
)

var errInvalidContentRange = errors.New("invalid content range")

type contentRange struct {
	start  int64
	length int64
}

func parseContentRange(headerValues []string, size int64) (contentRange, bool, error) {
	if len(headerValues) == 0 {
		return contentRange{length: size}, false, nil
	}
	if len(headerValues) != 1 || size <= 0 {
		return contentRange{}, false, errInvalidContentRange
	}

	unit, specification, found := strings.Cut(headerValues[0], "=")
	if !found || !strings.EqualFold(unit, "bytes") ||
		specification == "" || strings.Contains(specification, ",") {
		return contentRange{}, false, errInvalidContentRange
	}
	startValue, endValue, found := strings.Cut(specification, "-")
	if !found || strings.Contains(endValue, "-") {
		return contentRange{}, false, errInvalidContentRange
	}

	switch {
	case startValue == "":
		suffixLength, err := parseRangeNumber(endValue)
		if err != nil || suffixLength == 0 {
			return contentRange{}, false, errInvalidContentRange
		}
		if suffixLength > size {
			suffixLength = size
		}

		return contentRange{
			start:  size - suffixLength,
			length: suffixLength,
		}, true, nil

	case endValue == "":
		start, err := parseRangeNumber(startValue)
		if err != nil || start >= size {
			return contentRange{}, false, errInvalidContentRange
		}

		return contentRange{start: start, length: size - start}, true, nil

	default:
		start, err := parseRangeNumber(startValue)
		if err != nil || start >= size {
			return contentRange{}, false, errInvalidContentRange
		}
		end, err := parseRangeNumber(endValue)
		if err != nil || end < start {
			return contentRange{}, false, errInvalidContentRange
		}
		if end >= size {
			end = size - 1
		}

		return contentRange{start: start, length: end - start + 1}, true, nil
	}
}

func parseRangeNumber(value string) (int64, error) {
	if value == "" {
		return 0, errInvalidContentRange
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, errInvalidContentRange
		}
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errInvalidContentRange
	}

	return parsed, nil
}
