#!/bin/bash
# check.sh DIR: passes when every test in the shop repo passes and every test
# file is the original, byte for byte. Prints how many of the six bugs'
# tests pass.
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

fixed=0 open=""
for t in money:TestString cart:TestTotal discount:TestBuyXGetY tax:TestTax inventory:TestRelease receipt:TestFormat; do
	if gotest -count=1 -run "^${t#*:}\$" "./${t%%:*}" >/dev/null 2>&1; then
		fixed=$((fixed + 1))
	else
		open="$open ${t%%:*}"
	fi
done
[ -z "$changed" ] || fail "$fixed/6 bugs fixed, but test files changed:$changed"
[ $fixed = 6 ] || fail "$fixed/6 bugs fixed; still failing:$open"
out=$(gotest -count=1 ./... 2>&1) || fail "6/6 bugs fixed, but other tests fail: $(echo "$out" | grep -m3 -E 'FAIL|panic' | tr '\n' ' ')"
echo "PASS: 6/6 bugs fixed, all tests pass, test files untouched"
