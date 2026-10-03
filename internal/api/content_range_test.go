package api

import (
	"errors"
	"testing"
)

func TestParseContentRange(t *testing.T) {
	testCases := []struct {
		name        string
		header      []string
		size        int64
		expected    contentRange
		partial     bool
		expectError bool
	}{
		{
			name:     "complete representation",
			size:     100,
			expected: contentRange{length: 100},
		},
		{
			name:     "bounded range",
			header:   []string{"bytes=10-19"},
			size:     100,
			expected: contentRange{start: 10, length: 10},
			partial:  true,
		},
		{
			name:     "open range",
			header:   []string{"bytes=90-"},
			size:     100,
			expected: contentRange{start: 90, length: 10},
			partial:  true,
		},
		{
			name:     "suffix range",
			header:   []string{"bytes=-10"},
			size:     100,
			expected: contentRange{start: 90, length: 10},
			partial:  true,
		},
		{
			name:     "oversized end is clamped",
			header:   []string{"bytes=95-200"},
			size:     100,
			expected: contentRange{start: 95, length: 5},
			partial:  true,
		},
		{
			name:     "oversized suffix is clamped",
			header:   []string{"bytes=-200"},
			size:     100,
			expected: contentRange{length: 100},
			partial:  true,
		},
		{name: "empty file range", header: []string{"bytes=0-0"}, expectError: true},
		{name: "unsupported unit", header: []string{"items=0-1"}, size: 100, expectError: true},
		{name: "multiple ranges", header: []string{"bytes=0-1,3-4"}, size: 100, expectError: true},
		{name: "multiple headers", header: []string{"bytes=0-1", "bytes=3-4"}, size: 100, expectError: true},
		{name: "missing separator", header: []string{"bytes=10"}, size: 100, expectError: true},
		{name: "empty range", header: []string{"bytes=-"}, size: 100, expectError: true},
		{name: "negative syntax", header: []string{"bytes=-1-2"}, size: 100, expectError: true},
		{name: "signed number", header: []string{"bytes=+1-2"}, size: 100, expectError: true},
		{name: "reversed range", header: []string{"bytes=20-10"}, size: 100, expectError: true},
		{name: "start beyond size", header: []string{"bytes=100-"}, size: 100, expectError: true},
		{name: "zero suffix", header: []string{"bytes=-0"}, size: 100, expectError: true},
		{name: "overflow", header: []string{"bytes=999999999999999999999-"}, size: 100, expectError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			selected, partial, err := parseContentRange(testCase.header, testCase.size)
			if testCase.expectError {
				if !errors.Is(err, errInvalidContentRange) {
					t.Fatalf("expected errInvalidContentRange, got %v", err)
				}

				return
			}
			if err != nil {
				t.Fatalf("parse range: %v", err)
			}
			if selected != testCase.expected || partial != testCase.partial {
				t.Fatalf(
					"expected %+v partial=%t, got %+v partial=%t",
					testCase.expected,
					testCase.partial,
					selected,
					partial,
				)
			}
		})
	}
}
