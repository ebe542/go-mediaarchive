#!/usr/bin/env bash

# Validate the CI definition and reproduce its project checks locally.
set -Eeuo pipefail

readonly SCRIPT_NAME="$(basename "$0")"
readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

run_act=false
run_race=true

print_usage() {
	cat <<EOF
Usage: ${SCRIPT_NAME} [--act] [--skip-race] [--help]

Options:
  --act        Run the GitHub Actions verify job locally with act and Docker.
  --skip-race  Skip the race detector in the direct local checks.
  --help       Show this help text.
EOF
}

require_command() {
	local command_name="$1"
	local installation_hint="$2"

	if ! command -v "$command_name" >/dev/null 2>&1; then
		printf 'Error: required command %q was not found.\n' "$command_name" >&2
		printf 'Install it with: %s\n' "$installation_hint" >&2
		return 1
	fi
}

run_check() {
	local description="$1"
	shift

	printf '\n==> %s\n' "$description"
	"$@"
}

while (($# > 0)); do
	case "$1" in
	--act)
		run_act=true
		;;
	--skip-race)
		run_race=false
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

require_command \
	"actionlint" \
	"go install github.com/rhysd/actionlint/cmd/actionlint@latest"

run_check "GitHub Actions workflow syntax" actionlint

milestone_arguments=()
if [[ "$run_race" == true ]]; then
	milestone_arguments+=("--race")
fi

run_check \
	"CI project checks" \
	bash ./scripts/check_milestone.sh "${milestone_arguments[@]}"

if [[ "$run_act" == true ]]; then
	require_command "docker" "install Docker Desktop and enable its Linux engine"
	require_command "act" "winget install nektos.act"

	run_check "Docker availability" docker info
	run_check \
		"GitHub Actions verify job" \
		act pull_request --job verify
fi

printf '\nAll local CI checks passed.\n'
