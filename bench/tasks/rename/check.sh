#!/bin/bash
# check.sh DIR: passes when DIR is the tallyho project fully renamed to quokka,
# builds, passes every test a clean rename has, and prints the new version.
here=$(cd "$(dirname "$0")" && pwd)
dir=${1:?usage: check.sh DIR}
GO=${GO:-go}
export GOTOOLCHAIN=local GOFLAGS=-mod=mod
fail() { echo "FAIL: $*"; exit 1; }
gotest() { timeout -k 5 150 "$GO" test -timeout 60s "$@"; }  # a looping test fails in 60 s
cd "$dir" || fail "no directory $dir"

left=$(grep -r -i -l --exclude-dir=.git tallyho . | head -5)
[ -z "$left" ] || fail "the old name is still in: $(echo $left)"
paths=$(find . -path ./.git -prune -o -iname '*tallyho*' -print | head -5)
[ -z "$paths" ] || fail "the old name is still in paths: $(echo $paths)"
grep -q '^module example.com/quokka$' go.mod || fail "go.mod's module is not example.com/quokka"
[ -f cmd/quokka/main.go ] || fail "no cmd/quokka/main.go"
[ -f quokka.example.toml ] || fail "no quokka.example.toml"
grep -q 'bin/quokka' Makefile || fail "the Makefile does not build bin/quokka"
grep -q 'QUOKKA_HOME' README.md || fail "the README does not mention QUOKKA_HOME"

out=$(timeout -k 5 300 $GO build ./... 2>&1) || fail "build: $(echo "$out" | head -3 | tr '\n' ' ')"
out=$(gotest -count=1 ./... 2>&1) || fail "tests: $(echo "$out" | grep -m3 -E 'FAIL|panic|error|undefined' | tr '\n' ' ')"
v=$(timeout -k 5 120 $GO run ./cmd/quokka version 2>&1)
[ "$v" = "quokka 1.4.0" ] || fail "version prints '$v', want 'quokka 1.4.0'"

ref=$(mktemp -d)
trap 'rm -rf "$ref"' EXIT
bash "$here/make.sh" --solved "$ref/r" >/dev/null 2>&1 || fail "could not build the reference"
want=$(cd "$ref/r" && gotest -list . ./... 2>/dev/null | grep '^Test' | sort)
got=$(gotest -list . ./... 2>/dev/null | grep '^Test' | sort)
missing=$(comm -23 <(echo "$want") <(echo "$got"))
[ -z "$missing" ] || fail "tests missing after the rename: $(echo $missing)"
echo "PASS: renamed, builds, $(echo "$got" | wc -l | tr -d ' ') tests pass, prints quokka 1.4.0"
