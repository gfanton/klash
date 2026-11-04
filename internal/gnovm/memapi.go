// Copyright (C) 2026 Jae Kwon
//
// This file is part of gnosh's GnoVM machine subsystem. It is licensed under
// the GNO Network General Public License, version 6, as published by
// NewTendermint, LLC or its successor, or any later version of that License
// that the Licensor accepts as the proxy under its section 14, by a statement
// published in the canonical copy of internal/gnovm/LICENSE.md; together with
// the Additional Terms (Strong Attribution) in that file.

package gnovm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
)

// memMethod is one of mem's calls, or of a Frame's, as the model writes it:
// its name, its parameters, "(name, result...)", and its doc comment.
type memMethod struct {
	name, params, doc string
}

// memCalls is what the model may call on mem and on a Frame, in the order
// gnomem declares them. It is read from gnomem's own source, so that what a
// call that does not fit is told never disagrees with the library.
type memCalls struct {
	mem, frame []memMethod
}

var memAPI = readMemAPI()

// memNotCalls are exported for gnosh, not for the model: Bind is /goal's,
// Apply the journal's, Check the tests', Has gnosh mem's, NextHint the
// receipt's, and String prints.
var memNotCalls = map[string]bool{"Bind": true, "Apply": true, "Check": true, "Has": true, "NextHint": true, "String": true, "Name": true, "Kind": true, "Says": true, "NextTodo": true}

func readMemAPI() memCalls {
	var api memCalls
	files, err := fs.Glob(klashlibFS, "klashlibs/klash/gnomem/v2/*.gno")
	if err != nil {
		panic(err)
	}
	fset := token.NewFileSet()
	for _, path := range files {
		src, err := klashlibFS.ReadFile(path)
		if err != nil {
			panic(err)
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			panic(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() || memNotCalls[fn.Name.Name] {
				continue
			}
			call := memMethod{fn.Name.Name, memParams(fn.Type.Params), strings.Join(strings.Fields(fn.Doc.Text()), " ")}
			if t, ok := fn.Recv.List[0].Type.(*ast.StarExpr); ok {
				switch id, _ := t.X.(*ast.Ident); id.Name {
				case "Mem":
					api.mem = append(api.mem, call)
				case "Frame":
					api.frame = append(api.frame, call)
				}
			}
		}
	}
	return api
}

// memParams is a parameter list as the model writes the call: names only,
// with "..." after one that takes any number.
func memParams(list *ast.FieldList) string {
	var names []string
	for _, field := range list.List {
		_, many := field.Type.(*ast.Ellipsis)
		for _, n := range field.Names {
			name := strings.TrimSuffix(n.Name, "_")
			if many {
				names = append(names, name+"...")
			} else {
				names = append(names, name)
			}
		}
	}
	return "(" + strings.Join(names, ", ") + ")"
}

// find is the call named name, of mem's or of a Frame's.
func (calls memCalls) find(onFrame bool, name string) (memMethod, bool) {
	list := calls.mem
	if onFrame {
		list = calls.frame
	}
	for _, c := range list {
		if c.name == name {
			return c, true
		}
	}
	return memMethod{}, false
}

// names lists the calls of mem's, or of a Frame's, as a sentence does: "a,
// b and c".
func (calls memCalls) names(onFrame bool) string {
	list := calls.mem
	if onFrame {
		list = calls.frame
	}
	var names []string
	for _, c := range list {
		names = append(names, c.name)
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// memArgCount is what gno's "wrong argument count in call to <on>.<name>;
// want <want> got <got>" says in mem's words, from what the call takes; ""
// when name is no call of mem's or of a Frame's, and gno's words stand.
func memArgCount(on, name, want, got string) string {
	args := want + " arguments"
	if want == "1" {
		args = "1 argument"
	}
	if on == "mem" {
		c, ok := memAPI.find(false, name)
		if !ok {
			return ""
		}
		msg := "mem." + name + " takes " + args + ", not " + got
		if (name == "Fact" || name == "Rule") && got == "1" {
			return msg + ": mem." + name + "(name, text) sets a " + strings.ToLower(name) + ", and mem.Show(name) shows one"
		}
		return msg + ": mem." + name + c.params
	}
	// Anything else called on is a Frame, which mem.Todo and mem.Frame give.
	c, ok := memAPI.find(true, name)
	if !ok {
		return ""
	}
	msg := "a Frame's " + name + " takes " + args + ", not " + got + ": mem.Todo(…)." + name + c.params
	if mc, ok := memAPI.find(false, name); ok {
		msg += ", or mem." + name + mc.params + " for an item by its name"
	}
	return msg
}

// memHelp is what mem.Help(call) says: given "", the calls to know, with an
// example each, and the names of all; given a call's name, "Todo",
// "Frame.Push" or "Push", what it does and takes, from its doc comment in
// gnomem.
func memHelp(call string) string {
	if call == "" {
		return memHelpText + "mem.Help(\"Todo\") says what one call does and takes. mem's calls are " + memAPI.names(false) + "; a Frame's, on what mem.Todo(…) and mem.Frame(name) give, are " + memAPI.names(true) + ".\n"
	}
	name, onFrame := call, false
	if rest, ok := strings.CutPrefix(name, "Frame."); ok {
		name, onFrame = rest, true
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "mem."), "()")
	var b strings.Builder
	if c, ok := memAPI.find(false, name); ok && !onFrame {
		b.WriteString("mem." + c.name + c.params + ": " + c.doc + "\n")
	}
	if c, ok := memAPI.find(true, name); ok {
		b.WriteString("a Frame's " + c.name + c.params + ", as in mem.Todo(…)." + c.name + c.params + ": " + c.doc + "\n")
	}
	if b.Len() == 0 {
		return "mem has no call " + strconv.Quote(call) + ": mem's calls are " + memAPI.names(false) + "; a Frame's are " + memAPI.names(true) + "\n"
	}
	return b.String()
}

// memHelpText is mem.Help()'s list of the calls to know.
const memHelpText = `mem is your memory, kept by gnosh for this project. The calls to know:
  mem.Todo("make the port configurable", "add a --port flag", "document it").Push()  plan a task with its todos, in order, and step into it
  mem.Next()  step into the next todo; from the top, the first open one, in the order made
  mem.Pop("what came of it")  close the todo you are in, done, with its result
  mem.Leave("why")  step out of the todo you are in and leave it open
  mem.Frame("name").Push()  step into a todo by its name
  mem.Done("name", "what came of it")  close a todo by its name
  mem.Drop("name", "why")  give a todo up
  mem.Later("fix the flaky test")  a todo for later, at the top, from anywhere
  mem.Think("what you found")  keep a thought on the todo you are in; mem.Frame("name").Think("…") on another
  mem.Thought("name")  read a thought; mem.Recall("words") finds thoughts, todos and facts by meaning
  mem.Ask("name", "TOML or JSON?")  ask the user, under a todo
  mem.Todo("renew the TLS cert").Wait("2026-12-01")  a todo that waits for a day
  mem.Fact("cmd/test", "go test ./...")  keep a fact; mem.Fact("cmd/test") reads it
  mem.Rule("name", "…")  keep a standing instruction the user gave
  mem.Show(), mem.Show("name")  the outline, or one todo, fact or rule in full
  mem.Find("words")  search everything
`

// memMissing is what to tell the model of gno's "missing field <name> in
// <Mem or Frame>": a call mem's older versions had, where a Frame's call is
// mem's, or what there is to call.
func memMissing(name string, onFrame bool) string {
	if !onFrame {
		if hint, ok := memMoved[name]; ok {
			return hint
		}
		return "mem's calls are " + memAPI.names(false)
	}
	has := "a Frame, which mem.Todo(…) and mem.Frame(id) give, has " + memAPI.names(true)
	if c, ok := memAPI.find(false, name); ok {
		return has + "; " + name + " is mem's: mem." + name + c.params
	}
	return has
}
