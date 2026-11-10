#!/bin/bash
# make.sh [--solved] DIR: builds the tallyho project in DIR, a new git repo.
# --solved builds it already renamed to quokka: the reference check.sh uses.
set -euo pipefail
solved=0
if [ "${1:-}" = --solved ]; then solved=1; shift; fi
dir=${1:?usage: make.sh [--solved] DIR}
[ ! -e "$dir" ] || { echo "make.sh: $dir exists" >&2; exit 2; }
mkdir -p "$dir"
cd "$dir"
mkdir -p cmd/tallyho internal/store docs scripts .github/workflows

cat > go.mod <<'EOF'
module example.com/tallyho

go 1.21
EOF

cat > tallyho.go <<'EOF'
// Package tallyho counts lines, words and characters, and remembers totals
// between runs.
package tallyho

import (
	"bufio"
	"io"
	"strings"
	"unicode/utf8"
)

// Version is printed by `tallyho version`.
const Version = "tallyho 1.4.0"

// Counts is what Count returns for one input.
type Counts struct {
	Lines, Words, Chars int
}

// Tallyho counts inputs and keeps a running total.
type Tallyho struct {
	cfg   Config
	total Counts
}

// NewTallyho returns a Tallyho using cfg.
func NewTallyho(cfg Config) *Tallyho {
	return &Tallyho{cfg: cfg}
}

// Count counts r and adds the result to the running total.
func (t *Tallyho) Count(r io.Reader) (Counts, error) {
	var c Counts
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		c.Lines++
		c.Words += len(strings.Fields(line))
		c.Chars += utf8.RuneCountInString(line) + 1
	}
	if err := sc.Err(); err != nil {
		return Counts{}, err
	}
	t.total.Lines += c.Lines
	t.total.Words += c.Words
	t.total.Chars += c.Chars
	return c, nil
}

// Total is the sum of every Count so far.
func (t *Tallyho) Total() Counts { return t.total }
EOF

cat > config.go <<'EOF'
package tallyho

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// DefaultHome is where tallyho keeps its totals unless TALLYHO_HOME or the
// config file says otherwise.
const DefaultHome = "~/.tallyho"

// EnvHome names the environment variable that overrides the home directory.
const EnvHome = "TALLYHO_HOME"

// Config is read from a tallyho.toml file (see tallyho.example.toml).
type Config struct {
	Home    string
	Verbose bool
}

// LoadConfig reads the simple key = value file at path. A missing file gives
// the defaults. TALLYHO_HOME, when set, wins over the file.
func LoadConfig(path string) (Config, error) {
	cfg := Config{Home: DefaultHome}
	f, err := os.Open(path)
	if err != nil && !os.IsNotExist(err) {
		return cfg, err
	}
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				return cfg, fmt.Errorf("tallyho: bad config line %q", line)
			}
			k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"`)
			switch k {
			case "home":
				cfg.Home = v
			case "verbose":
				cfg.Verbose = v == "true"
			}
		}
		if err := sc.Err(); err != nil {
			return cfg, err
		}
	}
	if h := os.Getenv(EnvHome); h != "" {
		cfg.Home = h
	}
	return cfg, nil
}
EOF

cat > tallyho_test.go <<'EOF'
package tallyho

import (
	"strings"
	"testing"
)

func TestNewTallyho(t *testing.T) {
	th := NewTallyho(Config{Home: "/tmp/x"})
	if th.Total() != (Counts{}) {
		t.Fatalf("new Tallyho has total %+v", th.Total())
	}
}

func TestTallyhoCount(t *testing.T) {
	th := NewTallyho(Config{})
	c, err := th.Count(strings.NewReader("one two\nthree\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := (Counts{Lines: 2, Words: 3, Chars: 14}); c != want {
		t.Fatalf("Count = %+v, want %+v", c, want)
	}
	th.Count(strings.NewReader("four\n"))
	if got := th.Total().Words; got != 4 {
		t.Fatalf("total words = %d, want 4", got)
	}
}

func TestVersion(t *testing.T) {
	if !strings.HasPrefix(Version, "tallyho ") {
		t.Fatalf("Version = %q", Version)
	}
}
EOF

cat > config_test.go <<'EOF'
package tallyho

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv(EnvHome, "")
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Home != DefaultHome {
		t.Fatalf("Home = %q, want %q", cfg.Home, DefaultHome)
	}
}

func TestLoadConfigFile(t *testing.T) {
	t.Setenv(EnvHome, "")
	path := filepath.Join(t.TempDir(), "tallyho.toml")
	os.WriteFile(path, []byte("# tallyho settings\nhome = \"/var/lib/tallyho\"\nverbose = true\n"), 0o644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Home != "/var/lib/tallyho" || !cfg.Verbose {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadConfigEnvWins(t *testing.T) {
	t.Setenv("TALLYHO_HOME", "/srv/tallyho")
	cfg, err := LoadConfig(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Home != "/srv/tallyho" {
		t.Fatalf("Home = %q, want /srv/tallyho", cfg.Home)
	}
}
EOF

cat > cmd/tallyho/main.go <<'EOF'
// Command tallyho counts lines, words and characters in files.
//
//	tallyho count FILE...
//	tallyho version
package main

import (
	"fmt"
	"os"

	"example.com/tallyho"
	"example.com/tallyho/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: tallyho count FILE... | tallyho version")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(tallyho.Version)
	case "count":
		cfg, err := tallyho.LoadConfig("tallyho.toml")
		if err != nil {
			fmt.Fprintln(os.Stderr, "tallyho:", err)
			os.Exit(1)
		}
		th := tallyho.NewTallyho(cfg)
		for _, name := range os.Args[2:] {
			f, err := os.Open(name)
			if err != nil {
				fmt.Fprintln(os.Stderr, "tallyho:", err)
				os.Exit(1)
			}
			c, err := th.Count(f)
			f.Close()
			if err != nil {
				fmt.Fprintln(os.Stderr, "tallyho:", err)
				os.Exit(1)
			}
			fmt.Printf("%8d %8d %8d %s\n", c.Lines, c.Words, c.Chars, name)
		}
		st := store.Open(cfg.Home)
		st.Add(th.Total().Words)
		if cfg.Verbose {
			fmt.Printf("tallyho: %d words counted so far\n", st.Words())
		}
	default:
		fmt.Fprintf(os.Stderr, "tallyho: unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}
EOF

cat > internal/store/store.go <<'EOF'
// Package store keeps tallyho's running totals. It is in memory for now; the
// key names are what a disk store will use.
package store

import "fmt"

// KeyPrefix starts every key tallyho stores.
const KeyPrefix = "tallyho:"

// Store holds totals for one tallyho home directory.
type Store struct {
	home string
	kv   map[string]int
}

// Open returns the store for home.
func Open(home string) *Store {
	return &Store{home: home, kv: map[string]int{}}
}

// Add adds n to the words total.
func (s *Store) Add(n int) { s.kv[s.key("words")] += n }

// Words is the words total.
func (s *Store) Words() int { return s.kv[s.key("words")] }

func (s *Store) key(name string) string { return fmt.Sprintf("%s%s:%s", KeyPrefix, s.home, name) }
EOF

cat > internal/store/store_test.go <<'EOF'
package store

import "testing"

func TestStoreWords(t *testing.T) {
	s := Open("/tmp/tallyho-test")
	s.Add(3)
	s.Add(4)
	if s.Words() != 7 {
		t.Fatalf("Words = %d, want 7", s.Words())
	}
}

func TestKeyPrefix(t *testing.T) {
	s := Open("h")
	if got, want := s.key("words"), "tallyho:h:words"; got != want {
		t.Fatalf("key = %q, want %q", got, want)
	}
}
EOF

printf '%s\n' \
	'BIN := bin/tallyho' \
	'PREFIX ?= /usr/local' \
	'' \
	'.PHONY: build test install clean' \
	'' \
	'build:' \
	'	go build -o $(BIN) ./cmd/tallyho' \
	'' \
	'test:' \
	'	go test ./...' \
	'' \
	'install: build' \
	'	install -m 755 $(BIN) $(PREFIX)/bin/tallyho' \
	'' \
	'clean:' \
	'	rm -rf bin' > Makefile

cat > tallyho.example.toml <<'EOF'
# Example tallyho config. Copy to tallyho.toml next to where you run tallyho.
# TALLYHO_HOME, when set, overrides home.
home = "~/.tallyho"
verbose = false
EOF

cat > README.md <<'EOF'
# Tallyho

Tallyho counts lines, words and characters in files, like `wc`, and keeps a
running total of words across runs.

## Install

    make install

This builds `bin/tallyho` and copies it to `/usr/local/bin/tallyho`.

## Use

    tallyho count notes.txt todo.txt
    tallyho version

## Configure

Copy `tallyho.example.toml` to `tallyho.toml`. Set `TALLYHO_HOME` to keep
totals somewhere other than `~/.tallyho`.

See [docs/usage.md](docs/usage.md) for more.
EOF

cat > docs/usage.md <<'EOF'
# Using tallyho

`tallyho count FILE...` prints, for each file, its lines, words and
characters. With `verbose = true` in tallyho.toml, tallyho also prints the
running total of words it has counted.

`tallyho version` prints the version, such as `tallyho 1.4.0`.

Totals live under the home directory: `~/.tallyho` by default, or
`$TALLYHO_HOME`. Keys look like `tallyho:<home>:words`.
EOF

cat > scripts/release.sh <<'EOF'
#!/bin/sh
# Builds release tarballs of tallyho for Linux and macOS.
set -e
version=$(go run ./cmd/tallyho version | cut -d' ' -f2)
for os in linux darwin; do
	GOOS=$os GOARCH=amd64 go build -o dist/tallyho ./cmd/tallyho
	tar -C dist -czf "dist/tallyho-$version-$os-amd64.tar.gz" tallyho
	rm dist/tallyho
done
EOF
chmod +x scripts/release.sh

cat > .github/workflows/ci.yml <<'EOF'
name: tallyho CI
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: go test ./...
      - run: go build -o bin/tallyho ./cmd/tallyho
EOF

cat > CHANGELOG.md <<'EOF'
# Changelog

## 1.4.0
- tallyho remembers the words total between runs (TALLYHO_HOME).

## 1.3.0
- `tallyho version`.
EOF

if [ $solved = 1 ]; then
	grep -rl -i tallyho . | xargs perl -pi -e 's/tallyho/quokka/g; s/Tallyho/Quokka/g; s/TALLYHO/QUOKKA/g'
	find . -depth -name '*tallyho*' | while read -r p; do
		mv "$p" "$(dirname "$p")/$(basename "$p" | sed 's/tallyho/quokka/g')"
	done
fi

git init -q -b main
git add -A
git -c user.name=bench -c user.email=bench@example.com commit -q -m "tallyho 1.4.0"
