#!/bin/bash
# make.sh [--solved] DIR: builds the notes repo in DIR, a new git repo.
# --solved writes it with all six changes of prompt.txt made, and its own
# test updated for the new Delete: the reference check.sh is tested against.
set -euo pipefail
solved=0
if [ "${1:-}" = --solved ]; then solved=1; shift; fi
dir=${1:?usage: make.sh [--solved] DIR}
[ ! -e "$dir" ] || { echo "make.sh: $dir exists" >&2; exit 2; }
mkdir -p "$dir"
cd "$dir"

cat > go.mod <<'EOF'
module example.com/notes

go 1.21
EOF

cat > README.md <<'EOF'
# notes

A small in-memory store of text notes. Run the tests with `go test ./...`.
EOF

if [ $solved = 0 ]; then

cat > notes.go <<'EOF'
// Package notes keeps short text notes in memory.
package notes

// Note is one note.
type Note struct {
	ID   int
	Text string
}

// Store holds notes. Use NewStore to make one.
type Store struct {
	notes  []Note
	nextID int
}

// NewStore returns an empty store. IDs start at 1.
func NewStore() *Store { return &Store{nextID: 1} }

// Add stores a note and returns it.
func (s *Store) Add(text string) Note {
	n := Note{ID: s.nextID, Text: text}
	s.nextID++
	s.notes = append(s.notes, n)
	return n
}

// Get returns the note with id.
func (s *Store) Get(id int) (Note, bool) {
	for _, n := range s.notes {
		if n.ID == id {
			return n, true
		}
	}
	return Note{}, false
}

// Delete removes the note with id and reports whether it was there.
func (s *Store) Delete(id int) bool {
	for i, n := range s.notes {
		if n.ID == id {
			s.notes = append(s.notes[:i], s.notes[i+1:]...)
			return true
		}
	}
	return false
}

// List returns all notes in ID order.
func (s *Store) List() []Note {
	return append([]Note(nil), s.notes...)
}
EOF

cat > notes_test.go <<'EOF'
package notes

import "testing"

func TestAddGet(t *testing.T) {
	s := NewStore()
	a := s.Add("first")
	b := s.Add("second")
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("IDs = %d, %d; want 1, 2", a.ID, b.ID)
	}
	if n, ok := s.Get(2); !ok || n.Text != "second" {
		t.Fatalf("Get(2) = %+v, %v", n, ok)
	}
}

func TestDelete(t *testing.T) {
	s := NewStore()
	s.Add("a")
	s.Add("b")
	if !s.Delete(1) {
		t.Fatal("Delete(1) = false")
	}
	if s.Delete(1) {
		t.Fatal("a second Delete(1) = true")
	}
	if l := s.List(); len(l) != 1 || l[0].ID != 2 {
		t.Fatalf("List = %+v", l)
	}
}
EOF

else

cat > notes.go <<'EOF'
// Package notes keeps short text notes in memory.
package notes

import (
	"fmt"
	"strings"
)

// Note is one note.
type Note struct {
	ID   int
	Text string
	Tags []string
}

// Store holds notes. Use NewStore to make one.
type Store struct {
	notes  []Note
	nextID int
}

// NewStore returns an empty store. IDs start at 1.
func NewStore() *Store { return &Store{nextID: 1} }

// Add stores a note and returns it.
func (s *Store) Add(text string) Note {
	n := Note{ID: s.nextID, Text: text, Tags: tags(text)}
	s.nextID++
	s.notes = append(s.notes, n)
	return n
}

func tags(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range strings.Fields(text) {
		if len(w) < 2 || w[0] != '#' {
			continue
		}
		t := strings.ToLower(w[1:])
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// Get returns the note with id.
func (s *Store) Get(id int) (Note, bool) {
	for _, n := range s.notes {
		if n.ID == id {
			return n, true
		}
	}
	return Note{}, false
}

// Delete removes the listed notes and returns how many it removed.
func (s *Store) Delete(ids ...int) int {
	deleted := 0
	for _, id := range ids {
		for i, n := range s.notes {
			if n.ID == id {
				s.notes = append(s.notes[:i], s.notes[i+1:]...)
				deleted++
				break
			}
		}
	}
	return deleted
}

// List returns all notes in ID order.
func (s *Store) List() []Note {
	return append([]Note(nil), s.notes...)
}

// Search returns the notes whose text contains word, ignoring case.
func (s *Store) Search(word string) []Note {
	var out []Note
	w := strings.ToLower(word)
	for _, n := range s.notes {
		if strings.Contains(strings.ToLower(n.Text), w) {
			out = append(out, n)
		}
	}
	return out
}

// WithTag returns the notes that have tag.
func (s *Store) WithTag(tag string) []Note {
	tag = strings.ToLower(strings.TrimPrefix(tag, "#"))
	var out []Note
	for _, n := range s.notes {
		for _, t := range n.Tags {
			if t == tag {
				out = append(out, n)
				break
			}
		}
	}
	return out
}

// Stats returns the number of notes and of words in them.
func (s *Store) Stats() (notes, words int) {
	for _, n := range s.notes {
		words += len(strings.Fields(n.Text))
	}
	return len(s.notes), words
}

// Export returns one "ID<TAB>Text" line per note.
func (s *Store) Export() string {
	var b strings.Builder
	for _, n := range s.notes {
		fmt.Fprintf(&b, "%d\t%s\n", n.ID, n.Text)
	}
	return b.String()
}
EOF

cat > notes_test.go <<'EOF'
package notes

import "testing"

func TestAddGet(t *testing.T) {
	s := NewStore()
	a := s.Add("first")
	b := s.Add("second")
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("IDs = %d, %d; want 1, 2", a.ID, b.ID)
	}
	if n, ok := s.Get(2); !ok || n.Text != "second" {
		t.Fatalf("Get(2) = %+v, %v", n, ok)
	}
}

func TestDelete(t *testing.T) {
	s := NewStore()
	s.Add("a")
	s.Add("b")
	if s.Delete(1) != 1 {
		t.Fatal("Delete(1) != 1")
	}
	if s.Delete(1) != 0 {
		t.Fatal("a second Delete(1) != 0")
	}
	if l := s.List(); len(l) != 1 || l[0].ID != 2 {
		t.Fatalf("List = %+v", l)
	}
}
EOF

fi

git init -q -b main
git add -A
git -c user.name=bench -c user.email=bench@example.com commit -q -m "notes: add, get, delete, list"
