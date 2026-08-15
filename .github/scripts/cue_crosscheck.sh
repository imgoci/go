#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"

if ! command -v cue >/dev/null 2>&1; then
	if [[ -n "${CI:-}" ]]; then
		echo "cue cross-check: cue is not on PATH (required in CI)" >&2
		exit 1
	fi
	echo "skipping cue cross-check: cue is not on PATH"
	exit 0
fi

pin_file="testdata/conformance/SPEC_COMMIT"
if [[ ! -f "$pin_file" ]]; then
	echo "skipping cue cross-check: testdata/conformance/SPEC_COMMIT is missing"
	exit 0
fi

pin="$(tr -d '[:space:]' <"$pin_file")"
if [[ -z "$pin" ]]; then
	echo "skipping cue cross-check: testdata/conformance/SPEC_COMMIT is empty"
	exit 0
fi

local_spec="${HOME}/code/imgoci/spec"
schema=""
tmp_spec=""

cleanup() {
	if [[ -n "$tmp_spec" && -d "$tmp_spec" ]]; then
		rm -rf "$tmp_spec"
	fi
}
trap cleanup EXIT

if git -C "$local_spec" rev-parse HEAD >/dev/null 2>&1; then
	local_head="$(git -C "$local_spec" rev-parse HEAD)"
	if [[ "$local_head" == "$pin" && -f "$local_spec/schema/release-index-v1.cue" ]]; then
		schema="$local_spec/schema/release-index-v1.cue"
	fi
fi

if [[ -z "$schema" ]]; then
	tmp_spec="$(mktemp -d)"
	export GIT_TERMINAL_PROMPT=0
	if git -C "$tmp_spec" init --quiet \
		&& git -C "$tmp_spec" remote add origin https://github.com/imgoci/spec \
		&& git -C "$tmp_spec" fetch --depth 1 origin "$pin" \
		&& git -C "$tmp_spec" checkout --quiet --detach FETCH_HEAD \
		&& [[ -f "$tmp_spec/schema/release-index-v1.cue" ]]; then
		schema="$tmp_spec/schema/release-index-v1.cue"
	fi
fi

if [[ -z "$schema" || ! -f "$schema" ]]; then
	if [[ -n "${CI:-}" ]]; then
		echo "cue cross-check: could not obtain schema/release-index-v1.cue at ${pin} (required in CI)" >&2
		exit 1
	fi
	echo "skipping cue cross-check: could not obtain schema/release-index-v1.cue at ${pin}"
	exit 0
fi

shopt -s nullglob

require_json_count() {
	local dir="$1"
	local min="$2"
	if [[ ! -d "$dir" ]]; then
		echo "cue cross-check: missing directory: ${dir}" >&2
		exit 1
	fi
	local files=("$dir"/*.json)
	local n=${#files[@]}
	if [[ "$n" -lt "$min" ]]; then
		echo "cue cross-check: ${dir} has ${n} json files, expected >= ${min}" >&2
		exit 1
	fi
}

require_json_count testdata/conformance/v1/pass 12
require_json_count testdata/canonical/pass 12
require_json_count testdata/conformance/v1/fail 21

vet_fixture() {
	local fixture="$1"
	if ! cue vet -c -d '#ReleaseIndex' "$schema" "$fixture"; then
		echo "cue vet failed: ${fixture}" >&2
		exit 1
	fi
}

reject_fixture() {
	local fixture="$1"
	if cue vet -c -d '#ReleaseIndex' "$schema" "$fixture" >/dev/null 2>&1; then
		echo "cue vet unexpectedly succeeded on fail fixture: ${fixture}" >&2
		exit 1
	fi
}

for fixture in testdata/conformance/v1/pass/*.json; do
	vet_fixture "$fixture"
done

for fixture in testdata/canonical/pass/*.json; do
	vet_fixture "$fixture"
done

for fixture in testdata/conformance/v1/fail/*.json; do
	reject_fixture "$fixture"
done
