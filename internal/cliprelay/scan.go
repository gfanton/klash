// Package cliprelay runs a command on a pseudo-terminal of its own, joined
// to the terminal gnosh was started in, and puts the text of the OSC 52
// clipboard writes the command prints on this computer's clipboard. gnosh
// ssh is built on it: a terminal such as Apple's Terminal ignores OSC 52, so
// a copy made in gnosh, or in tmux, on the far side of ssh never reached the
// clipboard of the computer in front of you.
package cliprelay

import (
	"bytes"
	"encoding/base64"
)

// maxWrite is the most base64 one write may carry, about 6 MiB of text. A
// longer one still reaches the terminal; it is only not copied.
const maxWrite = 8 << 20

// keepBuffer is the largest buffer kept for the next write once one is
// done; a bigger one, left by a big copy, is let go.
const keepBuffer = 64 << 10

const (
	bel = 0x07
	esc = 0x1b
)

// oscPrefix starts the body of an OSC 52, the part after ESC ].
var oscPrefix = []byte("52;")

// Scanner finds the OSC 52 clipboard writes in a terminal's output, however
// the output is cut into pieces: ESC ] 52 ; Pc ; Pd, ended by BEL or by
// ESC \, where Pc names the clipboards (some of c, p, q, s and 0 to 7, or
// none) and Pd is the text in base64. A query, whose Pd is "?", and a clear,
// whose Pd is empty, are passed over: a program on the far side may write
// this computer's clipboard, never read it or empty it.
type Scanner struct {
	// Copy is called with the text of each write, in order.
	Copy func(text []byte)

	state scanState
	// body holds what follows ESC ] in an OSC that may yet be OSC 52.
	body []byte
}

type scanState uint8

const (
	ground     scanState = iota
	escape               // after ESC
	osc                  // in an OSC that may yet be OSC 52
	oscEscape            // after ESC in such an OSC: a backslash ends it
	skip                 // in some other OSC, or in one too long to copy
	skipEscape           // after ESC in such an OSC
)

// Write scans p, the next piece of the output. It always takes all of p.
func (s *Scanner) Write(p []byte) (int, error) {
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch s.state {
		case ground:
			j := bytes.IndexByte(p[i:], esc)
			if j < 0 {
				return len(p), nil
			}
			i += j
			s.state = escape
		case escape:
			switch b {
			case ']':
				s.drop(osc)
			case esc:
			default:
				s.state = ground
			}
		case osc:
			switch b {
			case bel:
				s.end()
			case esc:
				s.state = oscEscape
			default:
				s.body = append(s.body, b)
				n := len(s.body)
				if (n <= len(oscPrefix) && b != oscPrefix[n-1]) || n > maxWrite {
					s.drop(skip)
				}
			}
		case oscEscape:
			if b == '\\' {
				s.end()
				continue
			}
			// An escape that is not ST cuts the OSC short and starts a
			// sequence of its own, which this byte belongs to.
			s.drop(escape)
			i--
		case skip:
			j := bytes.IndexAny(p[i:], "\x07\x1b")
			if j < 0 {
				return len(p), nil
			}
			i += j
			if p[i] == esc {
				s.state = skipEscape
			} else {
				s.state = ground
			}
		case skipEscape:
			if b == '\\' {
				s.state = ground
				continue
			}
			s.state = escape
			i--
		}
	}
	return len(p), nil
}

// end ends an OSC that may be OSC 52.
func (s *Scanner) end() {
	if text, ok := clipboardText(s.body); ok {
		s.Copy(text)
	}
	s.drop(ground)
}

// drop forgets the OSC being read and moves to state.
func (s *Scanner) drop(state scanState) {
	s.state = state
	if cap(s.body) > keepBuffer {
		s.body = nil
	} else {
		s.body = s.body[:0]
	}
}

// clipboardText returns the text an OSC 52 body sets the clipboard to, or
// false for a body that does not set it.
func clipboardText(body []byte) ([]byte, bool) {
	rest, ok := bytes.CutPrefix(body, oscPrefix)
	if !ok {
		return nil, false
	}
	names, data, ok := bytes.Cut(rest, []byte{';'})
	if !ok || !clipboardNames(names) {
		return nil, false
	}
	// The decoders skip line breaks, so base64 as base64(1) writes it,
	// wrapped in lines, decodes all the same.
	text, err := base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		text, err = base64.RawStdEncoding.DecodeString(string(bytes.TrimRight(data, "=")))
	}
	// A query, "?", is not base64, and a clear decodes to nothing.
	if err != nil || len(text) == 0 {
		return nil, false
	}
	return text, true
}

// clipboardNames reports whether names is a Pc that OSC 52 allows.
func clipboardNames(names []byte) bool {
	for _, c := range names {
		if !bytes.ContainsRune([]byte("cpqs01234567"), rune(c)) {
			return false
		}
	}
	return true
}
