#!/bin/bash
# make.sh [--solved] DIR: builds the algos repo in DIR, a new git repo: nine
# small Go packages whose functions panic with "TODO" and whose tests fail.
# --solved writes the reference implementations instead of the stubs.
set -euo pipefail
solved=0
if [ "${1:-}" = --solved ]; then solved=1; shift; fi
dir=${1:?usage: make.sh [--solved] DIR}
[ ! -e "$dir" ] || { echo "make.sh: $dir exists" >&2; exit 2; }
mkdir -p "$dir"
cd "$dir"
mkdir -p stack lru roman rle brackets wordfreq matrix intervals topo

cat > go.mod <<'EOF'
module example.com/algos

go 1.21
EOF

cat > README.md <<'EOF'
# algos

Nine small packages: stack, lru, roman, rle, brackets, wordfreq, matrix,
intervals and topo. Each has tests. Run them with `go test ./...`.
EOF

# ---------------------------------------------------------------- tests

cat > stack/stack_test.go <<'EOF'
package stack

import "testing"

func TestEmpty(t *testing.T) {
	var s Stack[int]
	if s.Len() != 0 {
		t.Fatalf("Len = %d, want 0", s.Len())
	}
	if _, ok := s.Pop(); ok {
		t.Fatal("Pop on an empty stack: ok = true")
	}
	if _, ok := s.Peek(); ok {
		t.Fatal("Peek on an empty stack: ok = true")
	}
}

func TestOrder(t *testing.T) {
	var s Stack[string]
	for _, v := range []string{"a", "b", "c"} {
		s.Push(v)
	}
	if s.Len() != 3 {
		t.Fatalf("Len = %d, want 3", s.Len())
	}
	if v, ok := s.Peek(); !ok || v != "c" {
		t.Fatalf("Peek = %q, %v; want c, true", v, ok)
	}
	for _, want := range []string{"c", "b", "a"} {
		v, ok := s.Pop()
		if !ok || v != want {
			t.Fatalf("Pop = %q, %v; want %q, true", v, ok, want)
		}
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d after popping all, want 0", s.Len())
	}
}

func TestInterleaved(t *testing.T) {
	var s Stack[int]
	s.Push(1)
	s.Push(2)
	if v, _ := s.Pop(); v != 2 {
		t.Fatalf("Pop = %d, want 2", v)
	}
	s.Push(3)
	if v, _ := s.Pop(); v != 3 {
		t.Fatalf("Pop = %d, want 3", v)
	}
	if v, _ := s.Pop(); v != 1 {
		t.Fatalf("Pop = %d, want 1", v)
	}
	if _, ok := s.Pop(); ok {
		t.Fatal("Pop on an emptied stack: ok = true")
	}
}
EOF

cat > lru/lru_test.go <<'EOF'
package lru

import (
	"reflect"
	"testing"
)

func TestBasic(t *testing.T) {
	c := New(2)
	c.Put("a", 1)
	c.Put("b", 2)
	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Fatalf("Get(a) = %d, %v; want 1, true", v, ok)
	}
	c.Put("c", 3) // evicts b, the least recently used
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if v, ok := c.Get("c"); !ok || v != 3 {
		t.Fatalf("Get(c) = %d, %v; want 3, true", v, ok)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

func TestUpdateCountsAsUse(t *testing.T) {
	c := New(2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("a", 10) // a is now the most recent
	c.Put("c", 3)  // evicts b
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should have been evicted")
	}
	if v, _ := c.Get("a"); v != 10 {
		t.Fatalf("Get(a) = %d, want 10", v)
	}
	if c.Len() != 2 {
		t.Fatalf("Len = %d, want 2", c.Len())
	}
}

func TestKeysOrder(t *testing.T) {
	c := New(3)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	c.Get("a")
	if got, want := c.Keys(), []string{"a", "c", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
	c.Put("d", 4) // evicts b
	if got, want := c.Keys(), []string{"d", "a", "c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
}

func TestCapacityOne(t *testing.T) {
	c := New(1)
	c.Put("x", 1)
	c.Put("y", 2)
	if _, ok := c.Get("x"); ok {
		t.Fatal("x should have been evicted")
	}
	if v, ok := c.Get("y"); !ok || v != 2 {
		t.Fatalf("Get(y) = %d, %v; want 2, true", v, ok)
	}
}

func TestMissDoesNotChangeOrder(t *testing.T) {
	c := New(2)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Get("zzz")
	if got, want := c.Keys(), []string{"b", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Keys = %v, want %v", got, want)
	}
}
EOF

cat > roman/roman_test.go <<'EOF'
package roman

import "testing"

func TestToRoman(t *testing.T) {
	cases := map[int]string{1: "I", 4: "IV", 9: "IX", 14: "XIV", 40: "XL", 90: "XC",
		400: "CD", 1994: "MCMXCIV", 2024: "MMXXIV", 3999: "MMMCMXCIX"}
	for n, want := range cases {
		got, err := ToRoman(n)
		if err != nil || got != want {
			t.Errorf("ToRoman(%d) = %q, %v; want %q", n, got, err, want)
		}
	}
	for _, n := range []int{0, -1, 4000} {
		if _, err := ToRoman(n); err == nil {
			t.Errorf("ToRoman(%d): want an error", n)
		}
	}
}

func TestFromRoman(t *testing.T) {
	cases := map[string]int{"I": 1, "IV": 4, "IX": 9, "XLII": 42, "MCMXCIV": 1994, "MMMCMXCIX": 3999}
	for s, want := range cases {
		got, err := FromRoman(s)
		if err != nil || got != want {
			t.Errorf("FromRoman(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"", "IIII", "VV", "IC", "ABC", "MMMM", "iv"} {
		if _, err := FromRoman(s); err == nil {
			t.Errorf("FromRoman(%q): want an error", s)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for n := 1; n <= 3999; n++ {
		s, err := ToRoman(n)
		if err != nil {
			t.Fatalf("ToRoman(%d): %v", n, err)
		}
		m, err := FromRoman(s)
		if err != nil || m != n {
			t.Fatalf("FromRoman(%q) = %d, %v; want %d", s, m, err, n)
		}
	}
}
EOF

cat > rle/rle_test.go <<'EOF'
package rle

import "testing"

func TestEncode(t *testing.T) {
	cases := map[string]string{"": "", "a": "1a", "aaabccdddd": "3a1b2c4d", "abc": "1a1b1c",
		"zzzzzzzzzzzz": "12z", "ééé": "3é", "aaAA": "2a2A", "a  b": "1a2 1b"}
	for in, want := range cases {
		if got := Encode(in); got != want {
			t.Errorf("Encode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecode(t *testing.T) {
	cases := map[string]string{"": "", "1a": "a", "3a1b2c4d": "aaabccdddd", "12x": "xxxxxxxxxxxx", "3é": "ééé"}
	for in, want := range cases {
		got, err := Decode(in)
		if err != nil || got != want {
			t.Errorf("Decode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"a", "3", "0a", "3a2", "a3"} {
		if _, err := Decode(bad); err == nil {
			t.Errorf("Decode(%q): want an error", bad)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, s := range []string{"hello  world", "mississippi", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab"} {
		got, err := Decode(Encode(s))
		if err != nil || got != s {
			t.Errorf("Decode(Encode(%q)) = %q, %v", s, got, err)
		}
	}
}
EOF

cat > brackets/brackets_test.go <<'EOF'
package brackets

import "testing"

func TestBalanced(t *testing.T) {
	cases := map[string]bool{
		"":                         true,
		"()":                       true,
		"([]{})":                   true,
		"{[()()]}":                 true,
		"func(a []int) { return }": true,
		"a(b[c{d}e]f)g":            true,
		"([)]":                     false,
		"((":                       false,
		"))":                       false,
		"}{":                       false,
		"(]":                       false,
		"[[]":                      false,
	}
	for s, want := range cases {
		if got := Balanced(s); got != want {
			t.Errorf("Balanced(%q) = %v, want %v", s, got, want)
		}
	}
}
EOF

cat > wordfreq/wordfreq_test.go <<'EOF'
package wordfreq

import (
	"reflect"
	"testing"
)

func TestTopN(t *testing.T) {
	cases := []struct {
		text string
		n    int
		want []WordCount
	}{
		{"the cat and the hat", 2, []WordCount{{"the", 2}, {"and", 1}}},
		{"Don't stop. DON'T! don't", 1, []WordCount{{"don't", 3}}},
		{"'quoted' words 'here'", 5, []WordCount{{"here", 1}, {"quoted", 1}, {"words", 1}}},
		{"one, two; two... three: three three", 10, []WordCount{{"three", 3}, {"two", 2}, {"one", 1}}},
		{"Ünïcode ünïcode café CAFÉ", 2, []WordCount{{"café", 2}, {"ünïcode", 2}}},
		{"x '' y", 5, []WordCount{{"x", 1}, {"y", 1}}},
	}
	for _, c := range cases {
		if got := TopN(c.text, c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("TopN(%q, %d) = %v, want %v", c.text, c.n, got, c.want)
		}
	}
}

func TestTopNEmpty(t *testing.T) {
	if got := TopN("", 3); len(got) != 0 {
		t.Errorf("TopN(\"\", 3) = %v, want nothing", got)
	}
	if got := TopN("a b c", 0); len(got) != 0 {
		t.Errorf("TopN(\"a b c\", 0) = %v, want nothing", got)
	}
}
EOF

cat > matrix/matrix_test.go <<'EOF'
package matrix

import (
	"reflect"
	"testing"
)

func TestMultiply(t *testing.T) {
	a := Matrix{{1, 2, 3}, {4, 5, 6}}
	b := Matrix{{7, 8}, {9, 10}, {11, 12}}
	got, err := Multiply(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Matrix{{58, 64}, {139, 154}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Multiply = %v, want %v", got, want)
	}
	got, err = Multiply(a, Identity(3))
	if err != nil || !reflect.DeepEqual(got, a) {
		t.Fatalf("a × I = %v, %v; want %v", got, err, a)
	}
}

func TestMultiplyErrors(t *testing.T) {
	a := Matrix{{1, 2, 3}, {4, 5, 6}}
	if _, err := Multiply(a, a); err == nil {
		t.Error("2x3 × 2x3: want an error")
	}
	if _, err := Multiply(Matrix{{1, 2}, {3}}, Matrix{{1}, {2}}); err == nil {
		t.Error("ragged a: want an error")
	}
	if _, err := Multiply(Matrix{}, a); err == nil {
		t.Error("empty a: want an error")
	}
	if _, err := Multiply(Matrix{{1}}, Matrix{{1, 2}, {3}}); err == nil {
		t.Error("ragged b: want an error")
	}
}

func TestTranspose(t *testing.T) {
	got := Transpose(Matrix{{1, 2, 3}, {4, 5, 6}})
	if want := (Matrix{{1, 4}, {2, 5}, {3, 6}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("Transpose = %v, want %v", got, want)
	}
	if got := Transpose(Matrix{}); len(got) != 0 {
		t.Fatalf("Transpose(empty) = %v", got)
	}
}

func TestIdentity(t *testing.T) {
	if want := (Matrix{{1, 0, 0}, {0, 1, 0}, {0, 0, 1}}); !reflect.DeepEqual(Identity(3), want) {
		t.Fatalf("Identity(3) = %v", Identity(3))
	}
}
EOF

cat > intervals/intervals_test.go <<'EOF'
package intervals

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	cases := []struct{ in, want []Interval }{
		{[]Interval{{1, 3}, {2, 6}, {8, 10}, {15, 18}}, []Interval{{1, 6}, {8, 10}, {15, 18}}},
		{[]Interval{{1, 4}, {4, 5}}, []Interval{{1, 5}}},
		{[]Interval{{5, 6}, {1, 2}}, []Interval{{1, 2}, {5, 6}}},
		{[]Interval{{1, 2}, {3, 4}}, []Interval{{1, 2}, {3, 4}}},
		{[]Interval{{1, 10}, {2, 3}, {4, 5}}, []Interval{{1, 10}}},
		{[]Interval{{3, 3}}, []Interval{{3, 3}}},
		{[]Interval{{6, 8}, {1, 9}, {2, 4}, {4, 7}}, []Interval{{1, 9}}},
	}
	for _, c := range cases {
		if got := Merge(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Merge(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if got := Merge(nil); len(got) != 0 {
		t.Errorf("Merge(nil) = %v, want nothing", got)
	}
}

func TestMergeKeepsInput(t *testing.T) {
	in := []Interval{{5, 6}, {1, 2}, {2, 3}}
	orig := append([]Interval(nil), in...)
	Merge(in)
	if !reflect.DeepEqual(in, orig) {
		t.Fatalf("Merge changed its input to %v", in)
	}
}
EOF

cat > topo/topo_test.go <<'EOF'
package topo

import (
	"reflect"
	"testing"
)

func TestSort(t *testing.T) {
	cases := []struct {
		deps map[string][]string
		want []string
	}{
		{map[string][]string{"b": {"a"}, "c": {"b"}}, []string{"a", "b", "c"}},
		{map[string][]string{"x": nil}, []string{"x"}},
		{map[string][]string{"d": {"b", "c"}, "b": {"a"}, "c": {"a"}}, []string{"a", "b", "c", "d"}},
		{map[string][]string{"z": nil, "y": nil, "m": {"y"}}, []string{"y", "m", "z"}},
		{map[string][]string{"deploy": {"test", "build"}, "test": {"build"}, "build": {"fetch"}, "lint": {"fetch"}},
			[]string{"fetch", "build", "lint", "test", "deploy"}},
	}
	for _, c := range cases {
		got, err := Sort(c.deps)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Sort(%v) = %v, %v; want %v", c.deps, got, err, c.want)
		}
	}
}

func TestSortEmpty(t *testing.T) {
	got, err := Sort(map[string][]string{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Sort(empty) = %v, %v", got, err)
	}
}

func TestSortCycle(t *testing.T) {
	for _, deps := range []map[string][]string{
		{"a": {"b"}, "b": {"c"}, "c": {"a"}},
		{"a": {"a"}},
		{"x": nil, "p": {"q"}, "q": {"p"}},
	} {
		if got, err := Sort(deps); err == nil {
			t.Errorf("Sort(%v) = %v; want an error for the cycle", deps, got)
		}
	}
}
EOF

# ---------------------------------------------------------------- stubs

if [ $solved = 0 ]; then

cat > stack/stack.go <<'EOF'
// Package stack is a last-in, first-out stack.
package stack

// Stack holds values of any type. The zero value is an empty stack.
type Stack[T any] struct {
	items []T
}

// Push puts v on top.
func (s *Stack[T]) Push(v T) {
	panic("TODO")
}

// Pop removes and returns the top value. ok is false when the stack is empty.
func (s *Stack[T]) Pop() (v T, ok bool) {
	panic("TODO")
}

// Peek returns the top value without removing it. ok is false when the stack
// is empty.
func (s *Stack[T]) Peek() (v T, ok bool) {
	panic("TODO")
}

// Len is the number of values on the stack.
func (s *Stack[T]) Len() int {
	panic("TODO")
}
EOF

cat > lru/lru.go <<'EOF'
// Package lru is a fixed-capacity cache that evicts the least recently used key.
package lru

// Cache maps string keys to int values. Get (of a key that is there) and Put
// both count as a use of the key.
type Cache struct {
	// TODO
}

// New returns an empty cache holding at most capacity keys (capacity >= 1).
func New(capacity int) *Cache {
	panic("TODO")
}

// Get returns the value for key and marks key as the most recently used.
func (c *Cache) Get(key string) (int, bool) {
	panic("TODO")
}

// Put sets key to value and marks it the most recently used. When the cache is
// full and key is new, the least recently used key is evicted first.
func (c *Cache) Put(key string, value int) {
	panic("TODO")
}

// Len is the number of keys held.
func (c *Cache) Len() int {
	panic("TODO")
}

// Keys returns the keys from the most to the least recently used.
func (c *Cache) Keys() []string {
	panic("TODO")
}
EOF

cat > roman/roman.go <<'EOF'
// Package roman converts between integers and Roman numerals.
package roman

// ToRoman returns n (1 to 3999) as a Roman numeral in the standard subtractive
// form, such as 1994 -> "MCMXCIV". Any other n is an error.
func ToRoman(n int) (string, error) {
	panic("TODO")
}

// FromRoman parses a numeral in the standard form that ToRoman writes. It
// returns an error for an empty string, unknown letters (lower case letters
// are unknown), or a numeral that is not in standard form, such as "IIII",
// "VV" or "IC".
func FromRoman(s string) (int, error) {
	panic("TODO")
}
EOF

cat > rle/rle.go <<'EOF'
// Package rle is run-length encoding of text that has no digits in it.
package rle

// Encode writes each run of a repeated rune as its count followed by the rune:
// "aaabccdddd" -> "3a1b2c4d". The empty string encodes to "".
func Encode(s string) string {
	panic("TODO")
}

// Decode reverses Encode: "3a1b2c4d" -> "aaabccdddd", and "12x" -> twelve x's.
// It returns an error when s is not a valid encoding: a rune without a count
// before it, a count of zero, or a count with no rune after it.
func Decode(s string) (string, error) {
	panic("TODO")
}
EOF

cat > brackets/brackets.go <<'EOF'
// Package brackets checks that brackets are balanced.
package brackets

// Balanced reports whether every (, [ and { in s is closed by the matching ),
// ] or } in the right order, with no closing bracket left unmatched. Other
// characters are ignored.
func Balanced(s string) bool {
	panic("TODO")
}
EOF

cat > wordfreq/wordfreq.go <<'EOF'
// Package wordfreq counts words in text.
package wordfreq

// WordCount is a word and how many times it occurs.
type WordCount struct {
	Word  string
	Count int
}

// TopN returns the n most frequent words in text, most frequent first, ties
// broken by the word in alphabetical (byte) order. A word is a maximal run of
// letters (unicode.IsLetter) and apostrophes, lower-cased, with apostrophes at
// its start and end removed; a run that is then empty is not a word. If there
// are fewer than n words, all of them are returned. n <= 0 returns nothing.
func TopN(text string, n int) []WordCount {
	panic("TODO")
}
EOF

cat > matrix/matrix.go <<'EOF'
// Package matrix does small dense matrix arithmetic.
package matrix

// Matrix is a slice of rows; in a well-formed matrix every row has the same
// length. A matrix whose rows differ in length is ragged.
type Matrix [][]float64

// Multiply returns a×b. It is an error if either matrix is empty or ragged, or
// if a's column count differs from b's row count.
func Multiply(a, b Matrix) (Matrix, error) {
	panic("TODO")
}

// Transpose returns the transpose of m, which must not be ragged. An empty m
// transposes to an empty matrix.
func Transpose(m Matrix) Matrix {
	panic("TODO")
}

// Identity returns the n×n identity matrix.
func Identity(n int) Matrix {
	panic("TODO")
}
EOF

cat > intervals/intervals.go <<'EOF'
// Package intervals merges overlapping ranges.
package intervals

// Interval is the closed range [Start, End], with Start <= End.
type Interval struct{ Start, End int }

// Merge returns the union of the intervals as disjoint intervals sorted by
// Start. Intervals that overlap or touch (one's End equals another's Start)
// are merged; [1,2] and [3,4] do not touch. Merge must not modify its input.
func Merge(in []Interval) []Interval {
	panic("TODO")
}
EOF

cat > topo/topo.go <<'EOF'
// Package topo orders tasks so that each comes after the tasks it depends on.
package topo

// Sort returns every task named in deps (the keys and the tasks they list) in
// an order where each task comes after all of its dependencies; deps[t] lists
// the tasks t depends on. Whenever several tasks are ready (all their
// dependencies placed), the alphabetically first of them goes next, so the
// order is unique. A cycle is an error.
func Sort(deps map[string][]string) ([]string, error) {
	panic("TODO")
}
EOF

else

# ---------------------------------------------------------------- solutions

cat > stack/stack.go <<'EOF'
// Package stack is a last-in, first-out stack.
package stack

// Stack holds values of any type. The zero value is an empty stack.
type Stack[T any] struct {
	items []T
}

// Push puts v on top.
func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

// Pop removes and returns the top value. ok is false when the stack is empty.
func (s *Stack[T]) Pop() (v T, ok bool) {
	if len(s.items) == 0 {
		return v, false
	}
	v = s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}

// Peek returns the top value without removing it.
func (s *Stack[T]) Peek() (v T, ok bool) {
	if len(s.items) == 0 {
		return v, false
	}
	return s.items[len(s.items)-1], true
}

// Len is the number of values on the stack.
func (s *Stack[T]) Len() int { return len(s.items) }
EOF

cat > lru/lru.go <<'EOF'
// Package lru is a fixed-capacity cache that evicts the least recently used key.
package lru

import "container/list"

type entry struct {
	key   string
	value int
}

// Cache maps string keys to int values.
type Cache struct {
	cap int
	ll  *list.List
	m   map[string]*list.Element
}

// New returns an empty cache holding at most capacity keys.
func New(capacity int) *Cache {
	return &Cache{cap: capacity, ll: list.New(), m: map[string]*list.Element{}}
}

// Get returns the value for key and marks key as the most recently used.
func (c *Cache) Get(key string) (int, bool) {
	e, ok := c.m[key]
	if !ok {
		return 0, false
	}
	c.ll.MoveToFront(e)
	return e.Value.(*entry).value, true
}

// Put sets key to value and marks it the most recently used.
func (c *Cache) Put(key string, value int) {
	if e, ok := c.m[key]; ok {
		e.Value.(*entry).value = value
		c.ll.MoveToFront(e)
		return
	}
	if c.ll.Len() >= c.cap {
		old := c.ll.Back()
		c.ll.Remove(old)
		delete(c.m, old.Value.(*entry).key)
	}
	c.m[key] = c.ll.PushFront(&entry{key, value})
}

// Len is the number of keys held.
func (c *Cache) Len() int { return c.ll.Len() }

// Keys returns the keys from the most to the least recently used.
func (c *Cache) Keys() []string {
	var ks []string
	for e := c.ll.Front(); e != nil; e = e.Next() {
		ks = append(ks, e.Value.(*entry).key)
	}
	return ks
}
EOF

cat > roman/roman.go <<'EOF'
// Package roman converts between integers and Roman numerals.
package roman

import (
	"errors"
	"fmt"
	"strings"
)

var table = []struct {
	v int
	s string
}{{1000, "M"}, {900, "CM"}, {500, "D"}, {400, "CD"}, {100, "C"}, {90, "XC"},
	{50, "L"}, {40, "XL"}, {10, "X"}, {9, "IX"}, {5, "V"}, {4, "IV"}, {1, "I"}}

// ToRoman returns n (1 to 3999) as a Roman numeral.
func ToRoman(n int) (string, error) {
	if n < 1 || n > 3999 {
		return "", fmt.Errorf("roman: %d is out of range", n)
	}
	var b strings.Builder
	for _, p := range table {
		for n >= p.v {
			b.WriteString(p.s)
			n -= p.v
		}
	}
	return b.String(), nil
}

// FromRoman parses a numeral in standard form.
func FromRoman(s string) (int, error) {
	digit := map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	if s == "" {
		return 0, errors.New("roman: empty numeral")
	}
	n := 0
	for i := 0; i < len(s); i++ {
		v, ok := digit[s[i]]
		if !ok {
			return 0, fmt.Errorf("roman: unknown letter %q", s[i])
		}
		if i+1 < len(s) && digit[s[i+1]] > v {
			n -= v
		} else {
			n += v
		}
	}
	if back, err := ToRoman(n); err != nil || back != s {
		return 0, fmt.Errorf("roman: %q is not in standard form", s)
	}
	return n, nil
}
EOF

cat > rle/rle.go <<'EOF'
// Package rle is run-length encoding of text that has no digits in it.
package rle

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Encode writes each run of a repeated rune as its count followed by the rune.
func Encode(s string) string {
	var b strings.Builder
	rs := []rune(s)
	for i := 0; i < len(rs); {
		j := i
		for j < len(rs) && rs[j] == rs[i] {
			j++
		}
		b.WriteString(strconv.Itoa(j - i))
		b.WriteRune(rs[i])
		i = j
	}
	return b.String()
}

// Decode reverses Encode.
func Decode(s string) (string, error) {
	var b strings.Builder
	n := -1 // -1: no count yet
	for _, r := range s {
		if r >= '0' && r <= '9' {
			if n < 0 {
				n = 0
			}
			n = n*10 + int(r-'0')
			continue
		}
		if n < 0 {
			return "", fmt.Errorf("rle: %q has no count", r)
		}
		if n == 0 {
			return "", errors.New("rle: a count of zero")
		}
		b.WriteString(strings.Repeat(string(r), n))
		n = -1
	}
	if n >= 0 {
		return "", errors.New("rle: a count with no rune after it")
	}
	return b.String(), nil
}
EOF

cat > brackets/brackets.go <<'EOF'
// Package brackets checks that brackets are balanced.
package brackets

// Balanced reports whether the brackets in s are balanced.
func Balanced(s string) bool {
	open := map[rune]rune{')': '(', ']': '[', '}': '{'}
	var st []rune
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			st = append(st, r)
		case ')', ']', '}':
			if len(st) == 0 || st[len(st)-1] != open[r] {
				return false
			}
			st = st[:len(st)-1]
		}
	}
	return len(st) == 0
}
EOF

cat > wordfreq/wordfreq.go <<'EOF'
// Package wordfreq counts words in text.
package wordfreq

import (
	"sort"
	"strings"
	"unicode"
)

// WordCount is a word and how many times it occurs.
type WordCount struct {
	Word  string
	Count int
}

// TopN returns the n most frequent words in text.
func TopN(text string, n int) []WordCount {
	if n <= 0 {
		return nil
	}
	counts := map[string]int{}
	for _, w := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	}) {
		if w = strings.Trim(w, "'"); w != "" {
			counts[w]++
		}
	}
	out := make([]WordCount, 0, len(counts))
	for w, c := range counts {
		out = append(out, WordCount{w, c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Word < out[j].Word
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}
EOF

cat > matrix/matrix.go <<'EOF'
// Package matrix does small dense matrix arithmetic.
package matrix

import "errors"

// Matrix is a slice of rows.
type Matrix [][]float64

func cols(m Matrix) (int, bool) {
	if len(m) == 0 || len(m[0]) == 0 {
		return 0, false
	}
	for _, r := range m {
		if len(r) != len(m[0]) {
			return 0, false
		}
	}
	return len(m[0]), true
}

// Multiply returns a×b.
func Multiply(a, b Matrix) (Matrix, error) {
	ac, ok := cols(a)
	if !ok {
		return nil, errors.New("matrix: a is empty or ragged")
	}
	bc, ok := cols(b)
	if !ok {
		return nil, errors.New("matrix: b is empty or ragged")
	}
	if ac != len(b) {
		return nil, errors.New("matrix: a's columns differ from b's rows")
	}
	out := make(Matrix, len(a))
	for i := range a {
		out[i] = make([]float64, bc)
		for j := 0; j < bc; j++ {
			for k := 0; k < ac; k++ {
				out[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return out, nil
}

// Transpose returns the transpose of m.
func Transpose(m Matrix) Matrix {
	if len(m) == 0 {
		return Matrix{}
	}
	out := make(Matrix, len(m[0]))
	for j := range out {
		out[j] = make([]float64, len(m))
		for i := range m {
			out[j][i] = m[i][j]
		}
	}
	return out
}

// Identity returns the n×n identity matrix.
func Identity(n int) Matrix {
	out := make(Matrix, n)
	for i := range out {
		out[i] = make([]float64, n)
		out[i][i] = 1
	}
	return out
}
EOF

cat > intervals/intervals.go <<'EOF'
// Package intervals merges overlapping ranges.
package intervals

import "sort"

// Interval is the closed range [Start, End].
type Interval struct{ Start, End int }

// Merge returns the union of the intervals.
func Merge(in []Interval) []Interval {
	s := append([]Interval(nil), in...)
	sort.Slice(s, func(i, j int) bool { return s[i].Start < s[j].Start })
	var out []Interval
	for _, iv := range s {
		if n := len(out); n > 0 && iv.Start <= out[n-1].End {
			if iv.End > out[n-1].End {
				out[n-1].End = iv.End
			}
			continue
		}
		out = append(out, iv)
	}
	return out
}
EOF

cat > topo/topo.go <<'EOF'
// Package topo orders tasks so that each comes after the tasks it depends on.
package topo

import (
	"errors"
	"sort"
)

// Sort returns the tasks in dependency order, alphabetical among ready tasks.
func Sort(deps map[string][]string) ([]string, error) {
	need := map[string]int{}
	users := map[string][]string{}
	for t, ds := range deps {
		if _, ok := need[t]; !ok {
			need[t] = 0
		}
		for _, d := range ds {
			if _, ok := need[d]; !ok {
				need[d] = 0
			}
			need[t]++
			users[d] = append(users[d], t)
		}
	}
	var ready, out []string
	for t, n := range need {
		if n == 0 {
			ready = append(ready, t)
		}
	}
	for len(ready) > 0 {
		sort.Strings(ready)
		t := ready[0]
		ready = ready[1:]
		out = append(out, t)
		for _, u := range users[t] {
			if need[u]--; need[u] == 0 {
				ready = append(ready, u)
			}
		}
	}
	if len(out) != len(need) {
		return nil, errors.New("topo: the dependencies have a cycle")
	}
	return out, nil
}
EOF

fi

git init -q -b main
git add -A
git -c user.name=bench -c user.email=bench@example.com commit -q -m "algos: nine packages, tests first"
