#!/usr/bin/env bash

set -euo pipefail

version=""
output_directory="dist"

usage() {
	printf 'Usage: %s --version vMAJOR.MINOR.PATCH [--output-directory PATH]\n' "$0"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--version)
		if [[ $# -lt 2 ]]; then
			usage >&2
			exit 2
		fi
		version="$2"
		shift 2
		;;
	--output-directory)
		if [[ $# -lt 2 ]]; then
			usage >&2
			exit 2
		fi
		output_directory="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		printf 'Unknown argument: %s\n' "$1" >&2
		usage >&2
		exit 2
		;;
	esac
done

if [[ ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
	printf 'Version must use vMAJOR.MINOR.PATCH: %s\n' "$version" >&2
	exit 2
fi

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
project_root="$(cd "$script_directory/.." && pwd)"

if [[ "$output_directory" != /* ]]; then
	output_directory="$project_root/$output_directory"
fi
mkdir -p "$output_directory"

temporary_directory="$(mktemp -d)"
cleanup() {
	rm -rf -- "$temporary_directory"
}
trap cleanup EXIT

release_version="${version#v}"
targets=(
	"linux amd64"
	"linux arm64"
	"windows amd64"
	"darwin amd64"
	"darwin arm64"
)
commands=(server admin client)
archives=()

cd "$project_root"

for target in "${targets[@]}"; do
	read -r target_os target_arch <<<"$target"
	archive_base="go-mediaarchive_${release_version}_${target_os}_${target_arch}"
	package_directory="$temporary_directory/$archive_base"
	mkdir -p "$package_directory"

	for command_name in "${commands[@]}"; do
		binary_name="go-mediaarchive-$command_name"
		if [[ "$target_os" == "windows" ]]; then
			binary_name+=".exe"
		fi

		printf 'Building %s for %s/%s\n' "$command_name" "$target_os" "$target_arch"
		CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" \
			go build -trimpath \
			-o "$package_directory/$binary_name" \
			"./cmd/$command_name"
	done

	cp CHANGELOG.md LICENSE README.md "$package_directory/"

	if [[ "$target_os" == "windows" ]]; then
		archive_path="$output_directory/$archive_base.zip"
		rm -f -- "$archive_path"
		(
			cd "$temporary_directory"
			zip -q -r "$archive_path" "$archive_base"
		)
	else
		archive_path="$output_directory/$archive_base.tar.gz"
		tar -czf "$archive_path" -C "$temporary_directory" "$archive_base"
	fi

	archives+=("$archive_path")
done

checksum_path="$output_directory/SHA256SUMS"
rm -f -- "$checksum_path"
(
	cd "$output_directory"
	archive_names=()
	for archive_path in "${archives[@]}"; do
		archive_names+=("$(basename "$archive_path")")
	done
	sha256sum "${archive_names[@]}" >SHA256SUMS
)

printf 'Created %d release archives and %s\n' "${#archives[@]}" "$checksum_path"
