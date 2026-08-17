#!/usr/bin/env bash

# Run the quality checks required before completing a milestone.
set -Eeuo pipefail

readonly SCRIPT_NAME="$(basename "$0")"
readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

run_race_tests=false

print_usage() {
	cat <<EOF
Usage: ${SCRIPT_NAME} [--race] [--help]

Options:
  --race  Run the test suite with Go's race detector in addition to regular tests.
  --help  Show this help text.
EOF
}

while (($# > 0)); do
	case "$1" in
	--race)
		run_race_tests=true
		;;
	--help)
		print_usage
		exit 0
		;;
	*)
		printf 'Error: unknown argument: %s\n\n' "$1" >&2
		print_usage >&2
		exit 2
		;;
	esac
	shift
done

cd "$PROJECT_ROOT"

run_check() {
	local description="$1"
	shift

	printf '\n==> %s\n' "$description"
	"$@"
}

check_formatting() {
	local unformatted_files
	unformatted_files="$(gofmt -l .)"

	if [[ -n "$unformatted_files" ]]; then
		printf 'The following Go files require formatting:\n%s\n' "$unformatted_files" >&2
		printf 'Run: go fmt ./...\n' >&2
		return 1
	fi
}

check_git_whitespace() {
	git diff --check
	git diff --cached --check
}

printf 'Checking milestone in %s\n' "$PROJECT_ROOT"
run_check "Go version" go version
run_check "Go formatting" check_formatting
run_check "Module files" go mod tidy -diff
run_check "Static analysis" go vet ./...
run_check "Tests" go test -count=1 -cover ./...

if [[ "$run_race_tests" == true ]]; then
	run_check "Race detector" go test -count=1 -race ./...
fi

run_check "Build" go build ./...
run_check "Git whitespace" check_git_whitespace

printf '\nAll milestone checks passed.\n'
