// Copyright (C) 2026 Jae Kwon
//
// This file is part of gnosh's GnoVM machine subsystem. It is licensed under
// the GNO Network General Public License, version 6, as published by
// NewTendermint, LLC or its successor, or any later version of that License
// that the Licensor accepts as the proxy under its section 14, by a statement
// published in the canonical copy of internal/gnovm/LICENSE.md; together with
// the Additional Terms (Strong Attribution) in that file.

//go:build unix

package gnovm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The swarm's control (SWARM.md, demonstration A): the tree swarmdemo
// works on gno.land, groups of leaves under a parallel task, each group
// parallel, then each group's check and the task's, worked by N peers on
// one machine's local mem instead of a chain. Each peer is a process, as
// in the peers demonstration, with the same loop: Next; work; Pop and Next
// in one input. It only runs when asked, since it starts many processes:
//
//	GNOSH_PEERS_SWARM=10,100 GNOSH_PEER_WORK=1s \
//	    go test ./internal/gnovm/ -run TestPeersSwarmControl -v
//
// GNOSH_PEERS_SWARM_TREE=GxL sets the tree (default 10x100: 1,011 todos).
func TestPeersSwarmControl(t *testing.T) {
	if os.Getenv(peerEnv) == "1" {
		t.Skip("a peer process runs TestPeerChild only")
	}
	spec := os.Getenv("GNOSH_PEERS_SWARM")
	if spec == "" {
		t.Skip("set GNOSH_PEERS_SWARM (peer counts, e.g. 10,100) and GNOSH_PEER_WORK (e.g. 1s)")
	}
	groups, leaves := 10, 100
	if tr := os.Getenv("GNOSH_PEERS_SWARM_TREE"); tr != "" {
		g, l, ok := strings.Cut(tr, "x")
		require.True(t, ok, "GNOSH_PEERS_SWARM_TREE is GxL")
		var err error
		groups, err = strconv.Atoi(g)
		require.NoError(t, err)
		leaves, err = strconv.Atoi(l)
		require.NoError(t, err)
	}
	for _, ns := range strings.Split(spec, ",") {
		n, err := strconv.Atoi(ns)
		require.NoError(t, err)
		o := swarmControlRun(t, n, groups, leaves)
		results := o.results(t) // each todo entered and popped once, a parent after its todos
		require.Len(t, results, groups*leaves+groups+1)
		calls := 0
		for _, e := range o.events {
			if e.Kind == "pop" {
				calls++
			}
		}
		t.Logf("swarm control: %d peers, %d todos (%dx%d) of %s each: %s wall, %d pops",
			n, len(results), groups, leaves, peerWork("x"), o.wall.Round(100*time.Millisecond), calls)
	}
}

// swarmControlTree seeds the swarm's tree in project's mem, a group an
// input (one input for the whole tree runs out of gas), and returns the ids
// of its todos, by title, and the root's id.
func swarmControlTree(t *testing.T, dir, project string, groups, leaves int) (map[string]string, string) {
	h := newTestHost(t, dir)
	defer func() { require.NoError(t, h.Close()) }()
	ids := map[string]string{}
	added := regexp.MustCompile("added `([^`]+)` \"([^\"]+)\"")
	run := func(code string) {
		res, err := h.Run(context.Background(), RunRequest{Project: project, Name: MemVM, Code: code})
		require.NoError(t, err)
		require.NoError(t, res.Err, res.Output)
		for _, m := range added.FindAllStringSubmatch(res.Output, -1) {
			ids[m[2]] = m[1]
		}
	}
	run(`mem.Todo("the demo task").Parallel()`)
	root := ids["the demo task"]
	require.NotEmpty(t, root, "the task in mem")
	for g := 1; g <= groups; g++ {
		var b strings.Builder
		fmt.Fprintf(&b, "mem.Frame(%q).Todo(%q", root, fmt.Sprintf("group %d", g))
		for l := 1; l <= leaves; l++ {
			fmt.Fprintf(&b, ", %q", fmt.Sprintf("g%d todo %d", g, l))
		}
		b.WriteString(").Parallel()")
		run(b.String())
	}
	require.Len(t, ids, groups*leaves+groups+1, "the tree in mem")
	return ids, root
}

// swarmControlRun starts n peers, one process each, on a fresh copy of the
// swarm's tree, and waits for them all, as peersRun does for its tree.
func swarmControlRun(t *testing.T, n, groups, leaves int) peersOutcome {
	dir := t.TempDir()
	project := filepath.Join(dir, "proj")
	require.NoError(t, os.MkdirAll(project, 0o700))
	ids, root := swarmControlTree(t, dir, project, groups, leaves)
	start := time.Now()
	var cmds []*exec.Cmd
	var logs []string
	for i := 0; i < n; i++ {
		log := filepath.Join(dir, fmt.Sprintf("peer-%d.log", i))
		logs = append(logs, log)
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestPeerChild$", "-test.timeout=30m")
		cmd.Env = append(os.Environ(), peerEnv+"=1", "GNOSH_PEER_DIR="+dir, "GNOSH_PEER_PROJECT="+project,
			fmt.Sprintf("GNOSH_PEER_SESSION=peer%d", i+1), "GNOSH_PEER_ROLE=root", "GNOSH_PEER_ROOT="+root, "GNOSH_PEER_LOG="+log)
		out := &strings.Builder{}
		cmd.Stdout, cmd.Stderr = out, out
		require.NoError(t, cmd.Start())
		cmds = append(cmds, cmd)
		t.Cleanup(func() {
			if t.Failed() {
				t.Logf("peer%d:\n%s", i+1, out.String())
			}
		})
	}
	for _, cmd := range cmds {
		require.NoError(t, cmd.Wait())
	}
	o := peersOutcome{wall: time.Since(start), ids: ids, start: start}
	for _, log := range logs {
		f, err := os.Open(log)
		require.NoError(t, err)
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var e peerEvent
			require.NoError(t, json.Unmarshal(sc.Bytes(), &e))
			o.events = append(o.events, e)
		}
		f.Close()
	}
	slices.SortStableFunc(o.events, func(a, b peerEvent) int { return int(a.At - b.At) })
	_, entries, _, err := readMemJournal(filepath.Join(memDir(dir, project), "journal.jsonl"))
	require.NoError(t, err)
	o.entries = entries
	return o
}
