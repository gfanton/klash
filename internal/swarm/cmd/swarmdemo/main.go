// Command swarmdemo runs demo A of SWARM.md: N workers, each with its own
// key, work one plan on the shared mem realm through a chain, with no
// model: a todo's work is a sleep, so what is measured is the
// coordination. It is a demo and test tool; nothing in gnosh runs it.
//
//	swarmdemo keys -seed S -n 100 -amount 100000000000ugnot > balances.txt
//	swarmdemo run -rpc http://127.0.0.1:36657 -chain dev -seed S -n 100 \
//	    -groups 10 -leaves 20 -work 20s -work-max 40s -out run.json
//
// With -work-max, a todo's work takes between -work and -work-max, by a
// hash of its id, so every run does the same work. A worker sends Next
// once, then one PopNext per todo (pop it, take the next); -popnext=false
// sends Pop and then Next, two calls a todo. A todo's result is
// "did <id>", whoever does it, so every run of a plan should end with the
// same results; the report counts the results the chain took for each
// todo and digests them.
//
// Failures, for the runs that show what the realm does when things go
// wrong:
//
//	-kill 76-100@2m       workers 76 to 100 run in a child process, which
//	                      is killed (kill -9) 2 minutes in
//	-slow 3:100s,4:100s:renew -slow-after 1m
//	                      the first leaf worker 3 takes a minute in takes
//	                      it 100 s, more than a shorter lease; worker 4's
//	                      too, but it renews its hold as it goes (every
//	                      third of a lease, counted in blocks)
//	-halt-pid P -halt-at 2m -halt-for 1m
//	                      the chain's node, process P on this machine, is
//	                      frozen (SIGSTOP) for a minute, 2 minutes in
//
// As a machine's watcher would, the run sends Reclaim when a hold the
// chain gave has run out while one of its workers waits (-watch), since
// otherwise no call might come to take it back.
//
// Keys come from the seed, so the same seed makes the same worker keys the
// chain was funded with. They are demo keys: never fund them on a chain
// that matters.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/gnochain"
)

const defaultRealm = "gno.land/r/g1ecsuj0q572jr0dhu29q9njtnmw03hyu7tyyvv6/mem"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: swarmdemo keys|run [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keys":
		err = keys(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "swarmdemo:", err)
		os.Exit(1)
	}
}

func signer(seed, who string) *gnochain.KeySigner {
	s, err := gnochain.KeySignerFromSeed([]byte("swarmdemo/" + seed + "/" + who))
	if err != nil {
		panic(err)
	}
	return s
}

func keys(args []string) error {
	fs := flag.NewFlagSet("keys", flag.ExitOnError)
	seed := fs.String("seed", "swarm", "seed the demo keys come from")
	n := fs.Int("n", 10, "workers")
	amount := fs.String("amount", "100000000000ugnot", "each key's balance")
	_ = fs.Parse(args)
	fmt.Printf("%s=%s\n", signer(*seed, "owner").Address(), *amount)
	for i := 1; i <= *n; i++ {
		fmt.Printf("%s=%s\n", signer(*seed, "w"+strconv.Itoa(i)).Address(), *amount)
	}
	return nil
}

// --- the follower: one per run, as one per machine ---

type follower struct {
	mu       sync.Mutex
	plan     string
	start    time.Time
	outcomes map[string]gnochain.Outcome // tx hash → outcome, kept for late askers
	waiting  map[string]chan gnochain.Outcome
	assigned map[string][]assignment // holder → todos the realm gave it
	wake     map[string]chan struct{}
	relapse  map[string]bool     // holders whose place in a queue ran out: they ask again
	holds    map[string]int64    // todo → its hold's lease end (a block height), as the events say
	done     map[string][]string // todo → the results the chain took for it
	events   []eventStat
	blocks   []blockStat
	finished bool  // the plan's root is done: waiters get nothing more
	last     int64 // the last block handled
}

var (
	errFinished  = errors.New("the plan is finished")
	errWaitLapse = errors.New("the place in the queue ran out")
)

type assignment struct {
	ID    string
	Token int
	Title string
}

type blockStat struct {
	Height int64     `json:"height"`
	Time   time.Time `json:"time"`
	Txs    int       `json:"txs"`
	Failed int       `json:"failed"`
	Gas    int64     `json:"gas"`
}

// eventStat is one of the plan's events, as the chain emitted it.
type eventStat struct {
	T      float64 `json:"t"` // its block's time, seconds from the start
	Height int64   `json:"h"`
	Kind   string  `json:"k"`
	Todo   string  `json:"id"`              // the todo, or the frame a waiter waits in
	Worker int     `json:"w,omitempty"`     // the holder's worker number
	Until  int64   `json:"until,omitempty"` // a hold's or a place's lease end: a block height
}

func newFollower(plan string, start time.Time) *follower {
	return &follower{
		plan: plan, start: start,
		outcomes: map[string]gnochain.Outcome{}, waiting: map[string]chan gnochain.Outcome{},
		assigned: map[string][]assignment{}, wake: map[string]chan struct{}{},
		relapse: map[string]bool{}, holds: map[string]int64{}, done: map[string][]string{},
	}
}

func (f *follower) block(b gnochain.Block, realm string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := blockStat{Height: b.Height, Time: b.Time, Txs: len(b.Txs)}
	for _, o := range b.Txs {
		st.Gas += o.GasUsed
		if !o.OK {
			st.Failed++
		}
		h := string(o.Hash)
		f.outcomes[h] = o
		if ch, ok := f.waiting[h]; ok {
			ch <- o
			delete(f.waiting, h)
		}
		for _, e := range o.Events {
			if e.PkgPath == realm && e.Type == "mem" && e.Get("plan") == f.plan {
				f.event(b, e)
			}
		}
	}
	f.blocks = append(f.blocks, st)
	f.last = b.Height
	return nil
}

// event takes in one of the plan's events.
func (f *follower) event(b gnochain.Block, e gnochain.Event) {
	kind, id, holder := e.Get("kind"), e.Get("todo"), e.Get("holder")
	ev := eventStat{T: b.Time.Sub(f.start).Seconds(), Height: b.Height, Kind: kind, Todo: id, Worker: workerOf(holder)}
	var until int64
	if u := e.Get("until"); u != "" {
		until, _ = strconv.ParseInt(u, 10, 64)
		ev.Until = until
	}
	f.events = append(f.events, ev)
	switch kind {
	case "held", "assigned", "renewed":
		f.holds[id] = until
	case "done":
		delete(f.holds, id)
		f.done[id] = append(f.done[id], e.Get("result"))
	case "dropped", "released", "lapsed":
		delete(f.holds, id)
	case "waitlapsed":
		f.relapse[holder] = true
		f.wakeUp(holder)
	}
	if (kind == "done" || kind == "dropped") && id == "t" {
		f.finished = true
		for h := range f.wake {
			f.wakeUp(h)
		}
		return
	}
	if kind == "assigned" {
		tok, _ := strconv.Atoi(e.Get("seq"))
		f.assigned[holder] = append(f.assigned[holder], assignment{ID: id, Token: tok, Title: e.Get("title")})
		f.wakeUp(holder)
	}
}

func (f *follower) wakeUp(holder string) {
	if ch, ok := f.wake[holder]; ok {
		close(ch)
		delete(f.wake, holder)
	}
}

// outcome waits for the outcome of the transaction with hash h.
func (f *follower) outcome(ctx context.Context, h []byte) (gnochain.Outcome, error) {
	f.mu.Lock()
	if o, ok := f.outcomes[string(h)]; ok {
		f.mu.Unlock()
		return o, nil
	}
	ch := make(chan gnochain.Outcome, 1)
	f.waiting[string(h)] = ch
	f.mu.Unlock()
	select {
	case o := <-ch:
		return o, nil
	case <-ctx.Done():
		return gnochain.Outcome{}, ctx.Err()
	}
}

// assignment waits for the realm to give holder a todo, or to say its
// place in the queue ran out (errWaitLapse: ask again).
func (f *follower) assignment(ctx context.Context, holder string) (assignment, error) {
	for {
		f.mu.Lock()
		if as := f.assigned[holder]; len(as) > 0 {
			a := as[0]
			f.assigned[holder] = as[1:]
			delete(f.relapse, holder)
			f.mu.Unlock()
			return a, nil
		}
		if f.finished {
			f.mu.Unlock()
			return assignment{}, errFinished
		}
		if f.relapse[holder] {
			delete(f.relapse, holder)
			f.mu.Unlock()
			return assignment{}, errWaitLapse
		}
		ch, ok := f.wake[holder]
		if !ok {
			ch = make(chan struct{})
			f.wake[holder] = ch
		}
		f.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return assignment{}, ctx.Err()
		}
	}
}

// overdue says whether a worker of this run waits while a hold the chain
// gave has run out by the next block (a lease ends at a height): no call
// may come to take it back.
func (f *follower) overdue() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.wake) == 0 || f.finished {
		return false
	}
	for _, u := range f.holds {
		if u <= f.last+1 {
			return true
		}
	}
	return false
}

// height is the last block the follower handled.
func (f *follower) height() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}

// workerOf is the worker number in a holder ("<address>|-|w17"), or 0.
func workerOf(holder string) int {
	i := strings.LastIndex(holder, "|-|w")
	if i < 0 {
		return 0
	}
	n, _ := strconv.Atoi(holder[i+4:])
	return n
}

// --- the run ---

type todoStat struct {
	ID      string  `json:"id"`
	Worker  int     `json:"worker"`
	Claimed float64 `json:"claimed"` // seconds from the start: when the worker had it
	Done    float64 `json:"done"`    // when the chain had it done
	Work    float64 `json:"work"`    // seconds of work
	How     string  `json:"how"`     // held or assigned
}

type txStat struct {
	Kind    string  `json:"kind"`
	Gas     int64   `json:"gas"`
	OK      bool    `json:"ok"`
	Latency float64 `json:"latency"` // seconds from send to outcome
	Log     string  `json:"log,omitempty"`
}

// faultStat says which failures the run made, and when (seconds from the
// start).
type faultStat struct {
	Kill      string  `json:"kill,omitempty"` // the workers killed, "76-100"
	KillAt    float64 `json:"kill_at,omitempty"`
	Killed    float64 `json:"killed,omitempty"` // when kill -9 was sent
	Slow      string  `json:"slow,omitempty"`
	SlowAfter float64 `json:"slow_after,omitempty"`
	HaltAt    float64 `json:"halt_at,omitempty"`
	HaltFor   float64 `json:"halt_for,omitempty"`
	Halted    float64 `json:"halted,omitempty"`  // when the node was stopped
	Resumed   float64 `json:"resumed,omitempty"` // when it went on
}

// slowStat is a todo a slow worker took longer on than its work.
type slowStat struct {
	Worker int     `json:"worker"`
	Todo   string  `json:"id"`
	From   float64 `json:"from"`
	Work   float64 `json:"work"`
	Renew  bool    `json:"renew"`
	Renews int     `json:"renews"`
}

// lateStat is a pop the chain refused because the hold had run out.
type lateStat struct {
	Worker int     `json:"worker"`
	Todo   string  `json:"id"`
	T      float64 `json:"t"`
	Why    string  `json:"why"`
}

// resultStat is what the chain ended with: every todo should be done
// once, with "did <id>".
type resultStat struct {
	Todos  int    `json:"todos"`  // in the plan
	Done   int    `json:"done"`   // todos the chain took a result for
	Twice  int    `json:"twice"`  // todos it took more than one result for
	Wrong  int    `json:"wrong"`  // results other than "did <id>"
	Digest string `json:"digest"` // of the sorted "<id>\t<result>" lines: sha256, 16 hex digits
}

type report struct {
	Workers int         `json:"workers"`
	Groups  int         `json:"groups"`
	Leaves  int         `json:"leaves"`
	Work    float64     `json:"work_seconds"`
	WorkMax float64     `json:"work_max_seconds"`
	Check   float64     `json:"check_seconds"`
	WorkSum float64     `json:"work_sum_seconds"` // the plan's work, all todos
	PopNext bool        `json:"popnext"`
	Lease   int         `json:"lease"` // blocks
	Plan    string      `json:"plan"`
	Start   time.Time   `json:"start"`
	Wall    float64     `json:"wall_seconds"`
	Ideal   float64     `json:"ideal_seconds"`
	Todos   []todoStat  `json:"todos"`
	Txs     []txStat    `json:"txs"`
	Blocks  []blockStat `json:"blocks"`
	Setup   []txStat    `json:"setup"`
	Refused int64       `json:"refused"`
	Waits   int64       `json:"waits"`
	Faults  faultStat   `json:"faults"`
	Slow    []slowStat  `json:"slow,omitempty"`
	Late    []lateStat  `json:"late,omitempty"`
	Lapsed  int         `json:"lapsed"`
	Results resultStat  `json:"results"`
	Events  []eventStat `json:"events"`
}

type slowSpec struct {
	d     time.Duration
	renew bool
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	rpcURL := fs.String("rpc", "http://127.0.0.1:36657", "node RPC")
	chainID := fs.String("chain", "dev", "chain id")
	realm := fs.String("realm", defaultRealm, "the mem realm")
	seed := fs.String("seed", "swarm", "seed the demo keys come from")
	n := fs.Int("n", 10, "workers")
	groups := fs.Int("groups", 4, "parallel groups under the root")
	leaves := fs.Int("leaves", 10, "todos per group")
	work := fs.Duration("work", 2*time.Second, "time a todo's work takes (the least, with -work-max)")
	workMax := fs.Duration("work-max", 0, "the most a todo's work takes; each todo's is fixed by its id")
	check := fs.Duration("check", 0, "time a check takes (a group's todo, the root's: what has todos under it); 0: as -work")
	popNext := fs.Bool("popnext", true, "one PopNext a todo; false: Pop, then Next")
	lease := fs.Int("lease", 100, "lease, in blocks (gno.land makes a block every ~3 s)")
	gasNext := fs.Int64("gas-next", 60_000_000, "gas wanted for a Next (and a Renew or Reclaim)")
	gasPop := fs.Int64("gas-pop", 60_000_000, "gas wanted for a Pop")
	gasPopNext := fs.Int64("gas-popnext", 60_000_000, "gas wanted for a PopNext")
	every := fs.Duration("every", 200*time.Millisecond, "how often the follower asks for new blocks")
	conns := fs.Int("conns", 200, "sends at once (the node's RPC takes 900 connections)")
	maxDeposit := fs.Int64("max-deposit", 100_000_000, "storage deposit a call may lock, ugnot")
	out := fs.String("out", "", "write the report here (JSON)")
	planID := fs.String("plan", "", "plan id (default: one made from the time)")
	join := fs.Bool("join", false, "work the plan another run made (-plan): no setup, no watcher, no report")
	workersFlag := fs.String("workers", "", "which of the -n workers this run runs, first-last (default: all)")
	kill := fs.String("kill", "", "first-last@T: those workers run in a child process, killed (kill -9) T after the start")
	slow := fs.String("slow", "", "slow workers, comma-separated: i:D works D on the first leaf it takes after -slow-after; i:D:renew renews its hold meanwhile")
	slowAfter := fs.Duration("slow-after", time.Minute, "when the slow workers' slow todo starts")
	haltPID := fs.Int("halt-pid", 0, "the chain node's process on this machine, to freeze (SIGSTOP) for -halt-for at -halt-at")
	haltAt := fs.Duration("halt-at", time.Minute, "when to freeze the node")
	haltFor := fs.Duration("halt-for", 0, "how long the node stays frozen")
	watch := fs.Bool("watch", true, "send Reclaim when a hold has run out while one of this run's workers waits")
	_ = fs.Parse(args)
	if *join && *planID == "" {
		return errors.New("-join needs the -plan to work")
	}
	if *planID == "" {
		*planID = "demo-" + strconv.FormatInt(time.Now().Unix(), 36)
	}
	first, last := 1, *n
	if *workersFlag != "" {
		var err error
		if first, last, err = span(*workersFlag); err != nil {
			return fmt.Errorf("-workers: %w", err)
		}
	}
	killFirst, killLast, killAt := 0, -1, time.Duration(0)
	if *kill != "" {
		at := strings.IndexByte(*kill, '@')
		if at < 0 {
			return errors.New("-kill wants first-last@T")
		}
		var err error
		if killFirst, killLast, err = span((*kill)[:at]); err != nil {
			return fmt.Errorf("-kill: %w", err)
		}
		if killAt, err = time.ParseDuration((*kill)[at+1:]); err != nil {
			return fmt.Errorf("-kill: %w", err)
		}
	}
	slowOf := map[int]slowSpec{}
	for _, part := range strings.Split(*slow, ",") {
		if part == "" {
			continue
		}
		f := strings.Split(part, ":")
		i, err := strconv.Atoi(f[0])
		if err != nil || len(f) < 2 || len(f) > 3 || (len(f) == 3 && f[2] != "renew") {
			return fmt.Errorf("-slow: %q is not i:D or i:D:renew", part)
		}
		d, err := time.ParseDuration(f[1])
		if err != nil {
			return fmt.Errorf("-slow: %w", err)
		}
		slowOf[i] = slowSpec{d: d, renew: len(f) == 3}
	}
	ctx := context.Background()
	c, err := gnochain.Dial(ctx, *rpcURL, *chainID)
	if err != nil {
		return err
	}
	c.MaxDeposit = *maxDeposit
	c.Pool(*conns + 16)
	if *workMax < *work {
		*workMax = *work
	}
	workOf := func(id string) time.Duration {
		if *check > 0 && strings.Count(id, "/") < 2 {
			return *check
		}
		return workFor(id, *work, *workMax)
	}
	rep := &report{Workers: *n, Groups: *groups, Leaves: *leaves, Work: work.Seconds(), WorkMax: workMax.Seconds(), Check: check.Seconds(), PopNext: *popNext, Lease: *lease, Plan: *planID}
	rep.WorkSum = workOf("t").Seconds()
	for g := 1; g <= *groups; g++ {
		rep.WorkSum += workOf("t/g" + strconv.Itoa(g)).Seconds()
		for l := 1; l <= *leaves; l++ {
			rep.WorkSum += workOf("t/g" + strconv.Itoa(g) + "/l" + strconv.Itoa(l)).Seconds()
		}
	}

	if !*join {
		if err := setup(ctx, c, rep, *realm, *seed, *planID, *groups, *leaves, *lease); err != nil {
			return err
		}
	}

	// The follower, from the head on.
	head, err := c.Height(ctx)
	if err != nil {
		return err
	}
	start := time.Now()
	rep.Start = start
	since := func() float64 { return time.Since(start).Seconds() }
	fol := newFollower(*planID, start)
	fctx, stopFollow := context.WithCancel(ctx)
	defer stopFollow()
	fol.last = head
	go func() {
		// A failed request (the node busy, a connection dropped) must not
		// end the follower: every worker waits on it. Resume after the last
		// block handled.
		for fctx.Err() == nil {
			fol.mu.Lock()
			from := fol.last
			fol.mu.Unlock()
			err := c.Follow(fctx, from, *every, func(b gnochain.Block) error { return fol.block(b, *realm) })
			if fctx.Err() != nil {
				return
			}
			fmt.Fprintln(os.Stderr, "follower:", err, "(resuming)")
			time.Sleep(500 * time.Millisecond)
		}
	}()

	var mu sync.Mutex // guards rep from here on
	note := func(f func()) {
		mu.Lock()
		f()
		mu.Unlock()
	}

	// The watcher.
	if *watch && !*join {
		ws := c.WithSigner(signer(*seed, "owner")).Sender()
		go func() {
			for fctx.Err() == nil {
				time.Sleep(time.Second)
				if !fol.overdue() {
					continue
				}
				t0 := time.Now()
				sent, err := ws.Send(ctx, *gasNext, gnochain.MsgCallOf{PkgPath: *realm, Func: "Reclaim", Args: []string{*planID, "watcher"}})
				if err != nil {
					note(func() {
						rep.Txs = append(rep.Txs, txStat{Kind: "Reclaim", Latency: time.Since(t0).Seconds(), Log: err.Error()})
					})
					continue
				}
				o, err := fol.outcome(fctx, sent.Hash)
				if err != nil {
					return
				}
				note(func() {
					rep.Txs = append(rep.Txs, txStat{Kind: "Reclaim", Gas: o.GasUsed, OK: o.OK, Latency: time.Since(t0).Seconds(), Log: o.Log})
				})
				if !o.OK {
					ws.Forget()
				}
			}
		}()
	}

	// The workers to kill, in a process of their own.
	var child *exec.Cmd
	if *kill != "" && !*join {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cargs := append([]string{"run"}, args...)
		cargs = append(cargs, "-join", "-plan", *planID, "-workers", fmt.Sprintf("%d-%d", killFirst, killLast),
			"-kill", "", "-slow", "", "-halt-pid", "0", "-watch=false", "-out", "")
		child = exec.Command(exe, cargs...)
		child.Stdout, child.Stderr = os.Stderr, os.Stderr
		if err := child.Start(); err != nil {
			return fmt.Errorf("starting workers %d-%d: %w", killFirst, killLast, err)
		}
		go func() { _ = child.Wait() }()
		rep.Faults.Kill, rep.Faults.KillAt = fmt.Sprintf("%d-%d", killFirst, killLast), killAt.Seconds()
		time.AfterFunc(killAt, func() {
			_ = child.Process.Kill()
			t := since()
			note(func() { rep.Faults.Killed = t })
			fmt.Fprintf(os.Stderr, "killed workers %d-%d (kill -9, pid %d) at %.1fs\n", killFirst, killLast, child.Process.Pid, t)
		})
	}

	// The node to freeze.
	var halted, resumed atomic.Bool
	if *haltPID > 0 && *haltFor > 0 && !*join {
		rep.Faults.HaltAt, rep.Faults.HaltFor = haltAt.Seconds(), haltFor.Seconds()
		resume := func() {
			if halted.Load() && resumed.CompareAndSwap(false, true) {
				if err := resumeNode(*haltPID); err != nil {
					fmt.Fprintln(os.Stderr, "resuming the node:", err)
				}
				t := since()
				note(func() { rep.Faults.Resumed = t })
				fmt.Fprintf(os.Stderr, "node resumed at %.1fs\n", t)
			}
		}
		defer resume()
		time.AfterFunc(*haltAt, func() {
			if err := stopNode(*haltPID); err != nil {
				fmt.Fprintln(os.Stderr, "stopping the node:", err)
				return
			}
			halted.Store(true)
			t := since()
			note(func() { rep.Faults.Halted = t })
			fmt.Fprintf(os.Stderr, "node stopped at %.1fs\n", t)
			time.AfterFunc(*haltFor, resume)
		})
	}
	if *slow != "" {
		rep.Faults.Slow, rep.Faults.SlowAfter = *slow, slowAfter.Seconds()
	}

	// The workers, all at once.
	var wg sync.WaitGroup
	var refused, waits atomic.Int64
	gate := make(chan struct{}, *conns)
	for i := first; i <= last; i++ {
		if killFirst <= i && i <= killLast && !*join {
			continue // the child runs it
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			label := "w" + strconv.Itoa(i)
			ks := signer(*seed, label)
			holder := ks.Address().String() + "|-|" + label
			snd := c.WithSigner(ks).Sender()
			send := func(kind string, gas int64, calls ...gnochain.MsgCallOf) (gnochain.Outcome, error) {
				t0 := time.Now()
				gate <- struct{}{}
				sent, err := snd.Send(ctx, gas, calls...)
				<-gate
				if err != nil {
					refused.Add(1)
					note(func() {
						rep.Txs = append(rep.Txs, txStat{Kind: kind, OK: false, Latency: time.Since(t0).Seconds(), Log: err.Error()})
					})
					return gnochain.Outcome{}, err
				}
				o, err := fol.outcome(ctx, sent.Hash)
				note(func() {
					rep.Txs = append(rep.Txs, txStat{Kind: kind, Gas: o.GasUsed, OK: o.OK, Latency: time.Since(t0).Seconds(), Log: o.Log})
				})
				if err == nil && !o.OK {
					snd.Forget()
					return o, errors.New(o.Log)
				}
				return o, err
			}
			// sendGas sends, and once more with twice the gas when the
			// chain says the call wanted more than it was given.
			sendGas := func(kind string, gas int64, calls ...gnochain.MsgCallOf) (gnochain.Outcome, error) {
				o, err := send(kind, gas, calls...)
				if err != nil && strings.Contains(err.Error(), "exceeds tx's gas wanted") {
					return send(kind, 2*gas, calls...)
				}
				return o, err
			}
			// ask sends Next until the chain takes it (a busy node may refuse
			// a send for a while).
			ask := func() (gnochain.Outcome, bool) {
				for attempt := 0; ; attempt++ {
					o, err := sendGas("Next", *gasNext, gnochain.MsgCallOf{PkgPath: *realm, Func: "Next", Args: []string{*planID, label, ""}})
					if err == nil {
						return o, true
					}
					if attempt >= 20 {
						fmt.Fprintln(os.Stderr, label, "gives up:", firstLine(err.Error()))
						return o, false
					}
					time.Sleep(time.Second)
				}
			}
			slowness, isSlow := slowOf[i]
			o, ok := ask()
			for ok {
				parts := strings.Split(o.Values[0], "\t")
				var a assignment
				how := "held"
				switch parts[0] {
				case "held":
					tok, _ := strconv.Atoi(parts[2])
					a = assignment{ID: parts[1], Token: tok}
				case "waiting":
					waits.Add(1)
					var err error
					if a, err = fol.assignment(ctx, holder); err != nil {
						if errors.Is(err, errWaitLapse) {
							o, ok = ask()
							continue
						}
						return
					}
					how = "assigned"
				default: // nothing left
					return
				}
				claimed := since()
				w := workOf(a.ID)
				if isSlow && claimed >= slowAfter.Seconds() && strings.Count(a.ID, "/") == 2 {
					isSlow = false
					st := slowStat{Worker: i, Todo: a.ID, From: claimed, Work: slowness.d.Seconds(), Renew: slowness.renew}
					w = slowness.d
					if slowness.renew {
						// Renew every third of a lease, in blocks, as
						// gnosh's renewer does for a turn that goes on.
						end := time.Now().Add(w)
						step := max(int64(*lease/3), 1)
						last := fol.height()
						for time.Now().Before(end) {
							time.Sleep(min(250*time.Millisecond, time.Until(end)))
							if h := fol.height(); h >= last+step && time.Now().Before(end) {
								if _, err := sendGas("Renew", *gasNext, gnochain.MsgCallOf{PkgPath: *realm, Func: "Renew", Args: []string{*planID, label, a.ID, "", "false"}}); err != nil {
									fmt.Fprintln(os.Stderr, label, "renew", a.ID, reason(err.Error()))
								} else {
									st.Renews++
								}
								last = fol.height()
							}
						}
					} else {
						time.Sleep(w)
					}
					note(func() { rep.Slow = append(rep.Slow, st) })
				} else {
					time.Sleep(w)
				}
				result := "did " + a.ID
				var err error
				if *popNext {
					o, err = sendGas("PopNext", *gasPopNext, gnochain.MsgCallOf{PkgPath: *realm, Func: "PopNext", Args: []string{*planID, label, a.ID, strconv.Itoa(a.Token), result, ""}})
				} else {
					_, err = sendGas("Pop", *gasPop, gnochain.MsgCallOf{PkgPath: *realm, Func: "Pop", Args: []string{*planID, label, a.ID, strconv.Itoa(a.Token), result}})
				}
				if err != nil {
					why := reason(err.Error())
					fmt.Fprintln(os.Stderr, label, "pop", a.ID, why)
					if strings.Contains(why, "hold") {
						t := since()
						note(func() { rep.Late = append(rep.Late, lateStat{Worker: i, Todo: a.ID, T: t, Why: why}) })
					}
					o, ok = ask()
					continue
				}
				d := since()
				note(func() {
					rep.Todos = append(rep.Todos, todoStat{ID: a.ID, Worker: i, Claimed: claimed, Done: d, Work: w.Seconds(), How: how})
				})
				if !*popNext {
					o, ok = ask()
				}
			}
		}(i)
	}
	wg.Wait()
	wall := since()
	if *join {
		fmt.Printf("workers %d-%d of plan %s: done %d todos in %.1fs\n", first, last, *planID, len(rep.Todos), wall)
		return nil
	}
	stopFollow()
	mu.Lock()
	defer mu.Unlock()
	rep.Wall = wall
	fol.mu.Lock()
	rep.Blocks = fol.blocks
	rep.Events = fol.events
	rep.Results = tally(fol.done, *groups**leaves+*groups+1)
	fol.mu.Unlock()
	for _, e := range rep.Events {
		if e.Kind == "lapsed" {
			rep.Lapsed++
		}
	}
	rep.Refused, rep.Waits = refused.Load(), waits.Load()
	total := *groups**leaves + *groups + 1
	// The best any number of workers could do: the work spread evenly, or
	// the longest chain of todos that must go one after another (a leaf,
	// its group's check, the root's), whichever is longer.
	chainLen := 0.0
	for g := 1; g <= *groups; g++ {
		gid := "t/g" + strconv.Itoa(g)
		most := 0.0
		for l := 1; l <= *leaves; l++ {
			most = max(most, workOf(gid+"/l"+strconv.Itoa(l)).Seconds())
		}
		chainLen = max(chainLen, most+workOf(gid).Seconds())
	}
	chainLen += workOf("t").Seconds()
	rep.Ideal = max(rep.WorkSum/float64(*n), chainLen)
	fmt.Printf("workers %d, todos %d (done %d), work %.0fs, wall %.1fs, ideal %.1fs, waits %d, refused %d, blocks %d\n",
		*n, total, len(rep.Todos), rep.WorkSum, rep.Wall, rep.Ideal, rep.Waits, rep.Refused, len(rep.Blocks))
	fmt.Printf("results: %d of %d todos done, %d twice, %d wrong, digest %s; lapsed holds %d, late pops refused %d\n",
		rep.Results.Done, rep.Results.Todos, rep.Results.Twice, rep.Results.Wrong, rep.Results.Digest, rep.Lapsed, len(rep.Late))
	if *out != "" {
		bz, _ := json.MarshalIndent(rep, "", " ")
		return os.WriteFile(*out, bz, 0o644)
	}
	return nil
}

// setup makes the plan, its owner's calls each committed before the next
// (Call waits): a parallel root with groups parallel groups of leaves
// todos each.
func setup(ctx context.Context, c *gnochain.Client, rep *report, realm, seed, planID string, groups, leaves, lease int) error {
	owner := c.WithSigner(signer(seed, "owner"))
	call := func(fn string, args ...string) error {
		t0 := time.Now()
		r, err := owner.Call(ctx, realm, fn, 0, args...)
		rep.Setup = append(rep.Setup, txStat{Kind: fn, Gas: r.Gas, OK: err == nil, Latency: time.Since(t0).Seconds()})
		if err != nil {
			return fmt.Errorf("%s: %w", fn, err)
		}
		return nil
	}
	if err := call("NewPlan", planID, "the demo task: "+strconv.Itoa(groups*leaves)+" todos", "true", strconv.Itoa(lease)); err != nil {
		return err
	}
	if err := call("Parallel", planID, "owner", "t"); err != nil {
		return err
	}
	var gnames, gtitles []string
	for g := 1; g <= groups; g++ {
		gnames = append(gnames, "g"+strconv.Itoa(g))
		gtitles = append(gtitles, "group "+strconv.Itoa(g))
	}
	for i := 0; i < len(gnames); i += 64 {
		j := min(i+64, len(gnames))
		if err := call("AddTodos", planID, "owner", "t", strings.Join(gnames[i:j], "\n"), strings.Join(gtitles[i:j], "\n")); err != nil {
			return err
		}
	}
	for _, g := range gnames {
		if err := call("Parallel", planID, "owner", "t/"+g); err != nil {
			return err
		}
		var ln, lt []string
		for l := 1; l <= leaves; l++ {
			ln = append(ln, "l"+strconv.Itoa(l))
			lt = append(lt, g+" todo "+strconv.Itoa(l))
		}
		for i := 0; i < len(ln); i += 64 {
			j := min(i+64, len(ln))
			if err := call("AddTodos", planID, "owner", "t/"+g, strings.Join(ln[i:j], "\n"), strings.Join(lt[i:j], "\n")); err != nil {
				return err
			}
		}
	}
	return nil
}

// tally sums up the results the chain took: every one of the plan's todos
// todos should have one, "did <id>".
func tally(done map[string][]string, todos int) resultStat {
	st := resultStat{Todos: todos, Done: len(done)}
	lines := make([]string, 0, len(done))
	for id, rs := range done {
		if len(rs) > 1 {
			st.Twice++
		}
		if rs[0] != "did "+id {
			st.Wrong++
		}
		lines = append(lines, id+"\t"+rs[0])
	}
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	st.Digest = hex.EncodeToString(h[:8])
	return st
}

// span reads "first-last".
func span(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return 0, 0, fmt.Errorf("%q is not first-last", s)
	}
	first, err1 := strconv.Atoi(a)
	last, err2 := strconv.Atoi(b)
	if err1 != nil || err2 != nil || first < 1 || last < first {
		return 0, 0, fmt.Errorf("%q is not first-last", s)
	}
	return first, last, nil
}

// workFor is how long todo id's work takes: from least to most, by a hash
// of the id, so every run does the same work.
func workFor(id string, least, most time.Duration) time.Duration {
	if most <= least {
		return least
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	f := float64(h.Sum64()%10000) / 10000
	return least + time.Duration(f*float64(most-least))
}

// reason is what a refused transaction's log says why: the realm's
// panic, when it has one.
func reason(log string) string {
	for _, l := range strings.Split(log, "\n") {
		if !strings.Contains(l, "hold") {
			continue
		}
		// Data: errors.FmtError{format:"you do not hold …", args:…}
		if i := strings.Index(l, `format:"`); i >= 0 {
			l = l[i+len(`format:"`):]
			if j := strings.Index(l, `"`); j >= 0 {
				l = l[:j]
			}
		}
		return strings.TrimSpace(l)
	}
	return firstLine(log)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
