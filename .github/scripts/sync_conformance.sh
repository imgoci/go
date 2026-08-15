#!/usr/bin/env bash
# Sync imgoci spec conformance fixtures into testdata/conformance/v1.
#
# Default: clone https://github.com/imgoci/spec at the commit in
# testdata/conformance/SPEC_COMMIT and copy conformance/v1/{pass,fail}.
#   --pin <commit>  write SPEC_COMMIT then sync
#   --check         re-sync into a temp dir and diff against committed copies
set -euo pipefail

SPEC_REPO='https://github.com/imgoci/spec'
LOCAL_SPEC="${HOME}/code/imgoci/spec"

root=$(cd "$(dirname "$0")/../.." && pwd)
commit_file="${root}/testdata/conformance/SPEC_COMMIT"
dest="${root}/testdata/conformance/v1"

mode='sync'
pin_commit=''

usage() {
	echo "usage: $0 [--pin <commit>] [--check]" >&2
	exit 2
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--pin)
		[[ $# -ge 2 ]] || usage
		pin_commit=$2
		shift 2
		;;
	--check)
		mode='check'
		shift
		;;
	-h | --help)
		usage
		;;
	*)
		usage
		;;
	esac
done

read_commit() {
	local raw
	raw=$(tr -d '[:space:]' <"${commit_file}")
	if [[ -z ${raw} ]]; then
		echo "SPEC_COMMIT is empty: ${commit_file}" >&2
		exit 1
	fi
	printf '%s\n' "${raw}"
}

copy_fixtures() {
	local src=$1
	local out=$2
	if [[ ! -d ${src}/pass || ! -d ${src}/fail ]]; then
		echo "missing conformance/v1/{pass,fail} in ${src}" >&2
		return 1
	fi
	rm -rf "${out}/pass" "${out}/fail"
	mkdir -p "${out}"
	cp -R "${src}/pass" "${out}/pass"
	cp -R "${src}/fail" "${out}/fail"
}

clone_spec() {
	local dir=$1
	local commit=$2
	mkdir -p "${dir}"
	git init "${dir}" >/dev/null
	git -C "${dir}" remote add origin "${SPEC_REPO}"
	GIT_TERMINAL_PROMPT=0 git -C "${dir}" fetch --depth 1 origin "${commit}" >/dev/null
	git -C "${dir}" checkout --quiet --detach FETCH_HEAD
}

sync_from_clone() {
	local commit=$1
	local out=$2
	local tmp
	tmp=$(mktemp -d)
	# shellcheck disable=SC2064
	trap 'rm -rf "'"${tmp}"'"' RETURN
	clone_spec "${tmp}/spec" "${commit}" || return 1
	copy_fixtures "${tmp}/spec/conformance/v1" "${out}" || return 1
}

fallback_local() {
	local commit=$1
	local out=$2
	if [[ ! -d ${LOCAL_SPEC}/.git ]]; then
		echo "conformance sync: clone failed and ${LOCAL_SPEC} is not a git checkout" >&2
		return 2
	fi
	local head
	head=$(git -C "${LOCAL_SPEC}" rev-parse HEAD)
	if [[ ${head} != "${commit}" ]]; then
		echo "conformance sync: clone failed and ${LOCAL_SPEC} HEAD ${head} != SPEC_COMMIT ${commit}" >&2
		return 2
	fi
	echo "conformance sync: clone failed; using ${LOCAL_SPEC} at ${commit}" >&2
	copy_fixtures "${LOCAL_SPEC}/conformance/v1" "${out}"
}

if [[ -n ${pin_commit} ]]; then
	mkdir -p "$(dirname "${commit_file}")"
	printf '%s\n' "${pin_commit}" >"${commit_file}"
fi

commit=$(read_commit)

case "${mode}" in
sync)
	sync_from_clone "${commit}" "${dest}"
	;;
check)
	check_dir=$(mktemp -d)
	cleanup() { rm -rf "${check_dir}"; }
	trap cleanup EXIT
	set +e
	sync_from_clone "${commit}" "${check_dir}"
	clone_status=$?
	set -e
	if [[ ${clone_status} -ne 0 ]]; then
		set +e
		fallback_local "${commit}" "${check_dir}"
		fallback_status=$?
		set -e
		if [[ ${fallback_status} -eq 2 ]]; then
			if [[ -n ${CI:-} ]]; then
				echo "conformance check: clone failed and local fallback is unavailable/mismatched; drift gate cannot run in CI" >&2
				exit 1
			fi
			echo "conformance check: skipping (clone failed and local fallback unavailable/mismatched)" >&2
			exit 0
		fi
		if [[ ${fallback_status} -ne 0 ]]; then
			exit "${fallback_status}"
		fi
	fi
	diff -rq "${dest}/pass" "${check_dir}/pass"
	diff -rq "${dest}/fail" "${check_dir}/fail"
	;;
*)
	usage
	;;
esac
