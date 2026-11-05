// Copyright (C) 2026 Jae Kwon
//
// This file is part of gnosh's GnoVM machine subsystem. It is licensed under
// the GNO Network General Public License, version 6, as published by
// NewTendermint, LLC or its successor, or any later version of that License
// that the Licensor accepts as the proxy under its section 14, by a statement
// published in the canonical copy of internal/gnovm/LICENSE.md; together with
// the Additional Terms (Strong Attribution) in that file.

package gnomem

// Natives that gnosh provides. Each one that reads or changes mem panics
// unless it runs in a mem call, inside the realm klash.local/r/mem, with
// gnosh's own context, so a Mem the model's code makes or copies for itself
// can do nothing.

// caller is who runs this input: a session's id, "user" or "klash".
func caller() string

// today is the user's local date, "2026-10-29".
func today() string

// bound is the goal the user's /goal binds this input's session to; ""
// for none.
func bound() string

// frames is this input's session's stack of frames, bottom to top, as it
// stands: what gnosh derived from the session's own pushes without pops,
// with this input's pushes and pops so far.
func frames() []string

// fresh is the frame on top of this input's session's stack when the
// session entered it in its round before this input's, with nothing in
// between: a pop of it then would close a todo the model never did. ""
// otherwise.
func fresh() string

// pushed tells gnosh this input entered frame id, titled purpose; with id
// "", that a push found nothing to enter (Push() with nothing to enter).
func pushed(id, purpose string)

// popped tells gnosh this input left frame id, titled purpose, with
// result, holding thoughts thoughts.
func popped(id, purpose, result string, thoughts int)

// nameMade is the name gnosh made for text, when the agent gave none: its
// small model's answer, or "" when there is none; the realm checks it and
// falls back to the text's words.
func nameMade(text string) string

// record hands gnosh an Op of this call.
func record(kind, name, field, old, new, by string)

// pending is the Ops gnosh hands Apply, field by field, each with its
// call's time.
func pending() (kinds, names, fields, olds, news, bys []string, ats []int64)

// find is gnosh's index: the best matches for words, with their kinds
// ("item", "thought", "fact" or "rule"). how is "meaning" when it looked by
// meaning as well as by the words, which it does when gnosh has an
// embedding server; "words" when the server did not answer this search; ""
// otherwise.
func find(words string, max int) (names, kinds []string, how string)

// holding is where the journal, and this call's Ops, ever held text or its
// name form: names with their kinds, and whether statements of past inputs
// did.
func holding(text string) (names, kinds []string, statements bool)

// working reports whether session, another than this input's, is working on
// todo id now: in a busy stretch, with the todo, or one above it, on its
// stack (PARALLEL.md); with id "", whether it is in a busy stretch at all.
// false when gnosh cannot tell.
func working(session, id string) bool

// workKnown reports whether gnosh can tell which sessions are working:
// false with no turn locks to read (no data directory, or Windows), when
// working always answers false.
func workKnown() bool

// closing tells gnosh this input closes todo id, titled title, as done
// with result: a todo with no todo under it done (its todos, if any, were
// dropped or were questions the user answered), not an answer the user
// gave; thoughts are the thoughts kept on it, one a line. gnosh checks such a close before it keeps the input
// (CLOSE-CHECK.md): when it has not approved this one, it keeps nothing of
// the input, checks the close against what was done in the todo, and runs
// the input again or refuses it. The realm goes on as if it were kept.
func closing(id, title, result, thoughts string)

// mayWait reports whether this input's Next may wait for working sessions:
// its last statement is a bare mem.Next(), its session is not a sub-agent,
// and gnosh can wake it.
func mayWait() bool

// waitOn tells gnosh this input's Next waits for todo id, which session
// holds.
func waitOn(id, session string)

// subAgent reports whether this input's session is a sub-agent's, which
// works for its parent and never waits.
func subAgent() bool

// help is how to use mem: given "", the calls to know, with an example
// each; given a call's name, "Todo" or "Frame.Push", what it does and
// takes, from gnomem's own source. It answers anywhere.
func help(call string) string

// Two that only format, in Go, since a character at a time is slow in Gno;
// they answer anywhere.

// quote is strconv.Quote.
func quote(s string) string

// jsonString is s as a JSON string, with U+2028 and U+2029 escaped.
func jsonString(s string) string
