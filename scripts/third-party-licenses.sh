#!/usr/bin/env bash
# Writes THIRD_PARTY_LICENSES: the license notices of every module linked into
# ./cmd/gelato and of the Go standard library, which their licenses require to
# ship with the binaries. Run from the repository root (GoReleaser before hook).
set -euo pipefail

out=THIRD_PARTY_LICENSES
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

section() {
	printf '%s\n%s\n\n' "$1" "$(printf '%*s' "${#1}" '' | tr ' ' '=')"
}

modules=$(go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/gelato | sort -u)

while read -r path dir; do
	[[ -n $path ]] || continue

	shopt -s nullglob
	files=("$dir"/LICENSE* "$dir"/COPYING* "$dir"/NOTICE*)
	shopt -u nullglob

	if ((${#files[@]} == 0)); then
		echo "third-party-licenses: no license file in module $path ($dir)" >&2
		exit 1
	fi

	section "$path" >>"$tmp"
	for f in "${files[@]}"; do
		cat "$f" >>"$tmp"
		printf '\n' >>"$tmp"
	done
done <<<"$modules"

goroot=$(go env GOROOT)
golicense=$goroot/LICENSE
# Homebrew's go sets GOROOT to the keg's libexec and moves LICENSE to the keg root.
[[ -f $golicense ]] || golicense=$goroot/../LICENSE
if [[ ! -f $golicense ]]; then
	echo "third-party-licenses: no LICENSE in GOROOT ($goroot) or its parent" >&2
	exit 1
fi

section "Go standard library" >>"$tmp"
cat "$golicense" >>"$tmp"

chmod 644 "$tmp"
mv "$tmp" "$out"
trap - EXIT
