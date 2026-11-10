#!/bin/bash
# check.sh DIR: runs one hidden test per change of prompt.txt, each compiled
# alone in a copy of DIR, so a missing change fails only its own test. Passes
# when all six changes are done and the repo's own tests pass. Run after
# session 1 too: it then says which changes that session made (1 and 2 is
# the ask).
dir=${1:?usage: check.sh DIR}
GO=${GO:-go}
export GOTOOLCHAIN=local GOFLAGS=-mod=mod
fail() { echo "FAIL: $*"; exit 1; }
gotest() { timeout -k 5 150 "$GO" test -timeout 60s "$@"; }  # a looping test fails in 60 s
[ -d "$dir" ] || fail "no directory $dir"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

hidden() {
	case $1 in
	1) cat <<'EOF'
package notes

import (
	"reflect"
	"testing"
)

func TestCheck1Tags(t *testing.T) {
	s := NewStore()
	n := s.Add("Buy milk #Shopping #home #shopping")
	if !reflect.DeepEqual(n.Tags, []string{"shopping", "home"}) {
		t.Fatalf("Tags = %q, want [shopping home]", n.Tags)
	}
	if g, _ := s.Get(1); !reflect.DeepEqual(g.Tags, []string{"shopping", "home"}) {
		t.Fatalf("Get(1).Tags = %q", g.Tags)
	}
	if m := s.Add("no tags # here"); m.Tags != nil {
		t.Fatalf("Tags = %q, want nil", m.Tags)
	}
}
EOF
	;;
	2) cat <<'EOF'
package notes

import "testing"

func TestCheck2Search(t *testing.T) {
	s := NewStore()
	s.Add("Buy MILK")
	s.Add("call mom")
	s.Add("milkshake recipe")
	got := s.Search("milk")
	if len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("Search(milk) = %+v, want notes 1 and 3", got)
	}
	if got := s.Search("zebra"); len(got) != 0 {
		t.Fatalf("Search(zebra) = %+v", got)
	}
}
EOF
	;;
	3) cat <<'EOF'
package notes

import "testing"

func TestCheck3Delete(t *testing.T) {
	s := NewStore()
	s.Add("a")
	s.Add("b")
	s.Add("c")
	var got int = s.Delete(1, 3, 9)
	if got != 2 {
		t.Fatalf("Delete(1, 3, 9) = %d, want 2", got)
	}
	if l := s.List(); len(l) != 1 || l[0].ID != 2 {
		t.Fatalf("List = %+v, want note 2 only", l)
	}
	if got := s.Delete(); got != 0 {
		t.Fatalf("Delete() = %d, want 0", got)
	}
}
EOF
	;;
	4) cat <<'EOF'
package notes

import "testing"

func TestCheck4WithTag(t *testing.T) {
	s := NewStore()
	s.Add("a #Work")
	s.Add("b #home")
	s.Add("c #work #home")
	if got := s.WithTag("WORK"); len(got) != 2 || got[0].ID != 1 || got[1].ID != 3 {
		t.Fatalf("WithTag(WORK) = %+v, want notes 1 and 3", got)
	}
	if got := s.WithTag("#home"); len(got) != 2 || got[0].ID != 2 || got[1].ID != 3 {
		t.Fatalf("WithTag(#home) = %+v, want notes 2 and 3", got)
	}
	if got := s.WithTag("play"); len(got) != 0 {
		t.Fatalf("WithTag(play) = %+v", got)
	}
}
EOF
	;;
	5) cat <<'EOF'
package notes

import "testing"

func TestCheck5Stats(t *testing.T) {
	s := NewStore()
	if n, w := s.Stats(); n != 0 || w != 0 {
		t.Fatalf("Stats of an empty store = %d, %d", n, w)
	}
	s.Add("one two  three")
	s.Add("four")
	if n, w := s.Stats(); n != 2 || w != 4 {
		t.Fatalf("Stats = %d, %d; want 2, 4", n, w)
	}
}
EOF
	;;
	6) cat <<'EOF'
package notes

import "testing"

func TestCheck6Export(t *testing.T) {
	s := NewStore()
	if got := s.Export(); got != "" {
		t.Fatalf("Export of an empty store = %q", got)
	}
	s.Add("Buy milk #shopping")
	s.Add("call mom")
	if got, want := s.Export(), "1\tBuy milk #shopping\n2\tcall mom\n"; got != want {
		t.Fatalf("Export = %q, want %q", got, want)
	}
}
EOF
	;;
	esac
}

done_list="" missing=""
n=0
for i in 1 2 3 4 5 6; do
	rm -rf "$work/c"
	mkdir "$work/c"
	(cd "$dir" && tar --exclude=.git -cf - .) | (cd "$work/c" && tar -xf -)
	hidden $i > "$work/c/zz_check${i}_test.go"
	if (cd "$work/c" && gotest -count=1 -run "^TestCheck${i}" . >/dev/null 2>&1); then
		n=$((n + 1))
		done_list="$done_list $i"
	else
		missing="$missing $i"
	fi
done
[ $n = 6 ] || fail "$n/6 changes done (done:${done_list:- none}; missing:$missing)"
out=$(cd "$dir" && gotest -count=1 ./... 2>&1) || fail "6/6 changes done, but the repo's own tests fail: $(echo "$out" | grep -m3 -E 'FAIL|panic|error' | tr '\n' ' ')"
echo "PASS: 6/6 changes done, the repo's tests pass"
