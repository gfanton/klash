#!/bin/bash
# check.sh DIR: passes when all nine packages pass their tests and every test
# file is the original, byte for byte. Prints how many packages pass.
here=$(cd "$(dirname "$0")" && pwd)
dir=${1:?usage: check.sh DIR}
GO=${GO:-go}
export GOTOOLCHAIN=local GOFLAGS=-mod=mod
fail() { echo "FAIL: $*"; exit 1; }
gotest() { timeout -k 5 150 "$GO" test -timeout 60s "$@"; }  # a looping test fails in 60 s
cd "$dir" || fail "no directory $dir"

ref=$(mktemp -d)
trap 'rm -rf "$ref"' EXIT
bash "$here/make.sh" "$ref/r" >/dev/null 2>&1 || fail "could not build the reference"
changed=""
for f in $(cd "$ref/r" && find . -name '*_test.go' | sort); do
	cmp -s "$ref/r/$f" "$f" || changed="$changed ${f#./}"
done

pkgs="stack lru roman rle brackets wordfreq matrix intervals topo"
passed=0 failing=""
for p in $pkgs; do
	if gotest -count=1 "./$p" >/dev/null 2>&1; then
		passed=$((passed + 1))
	else
		failing="$failing $p"
	fi
done
[ -z "$changed" ] || fail "$passed/9 packages pass, but test files changed:$changed"
[ $passed = 9 ] || fail "$passed/9 packages pass; failing:$failing"
echo "PASS: 9/9 packages pass, test files untouched"
