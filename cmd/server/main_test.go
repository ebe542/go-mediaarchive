package main

import "testing"

func TestDatabasePathFromEnvironment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		environment  string
		expectedPath string
	}{
		{
			name:         "environment value",
			environment:  "runtime/archive.db",
			expectedPath: "runtime/archive.db",
		},
		{
			name:         "built-in default",
			environment:  "",
			expectedPath: defaultDatabasePath,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			actualPath := databasePathFromEnvironment(
				func(name string) string {
					if name == "MEDIAARCHIVE_DATABASE" {
						return test.environment
					}

					return ""
				},
			)

			if actualPath != test.expectedPath {
				t.Errorf(
					"expected database path %q, got %q",
					test.expectedPath,
					actualPath,
				)
			}
		})
	}
}
