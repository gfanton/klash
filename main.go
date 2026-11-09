// Package main is the entry point for the Crush CLI.
package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"

	"github.com/charmbracelet/crush/internal/cmd"
	_ "github.com/charmbracelet/crush/internal/dns"
	"github.com/charmbracelet/crush/internal/env"
	"github.com/charmbracelet/crush/internal/gnovm"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	// The GnoVM worker gnosh spawns for its VMs (internal/gnovm), before
	// anything else: it does nothing without its client's handshake.
	if len(os.Args) > 1 && os.Args[1] == "gnovm-worker" {
		os.Exit(gnovm.WorkerMain())
	}
	// gnosh ssh hands its arguments to ssh as they are and runs for as long
	// as the ssh session does, so none of the start below is for it: above
	// all not the pprof server, whose port it would hold all that time.
	if len(os.Args) > 1 && os.Args[1] == "ssh" {
		os.Exit(cmd.SSH(os.Args[2:]))
	}
	if setting := env.Getenv("GNOSH_PROFILE"); setting != "" {
		go servePprof(setting)
	}

	cmd.Execute()
}

// pprofDefaultAddr is where GNOSH_PROFILE=1 serves pprof.
const pprofDefaultAddr = "localhost:6060"

// servePprof serves net/http/pprof for GNOSH_PROFILE; see pprofListen.
func servePprof(setting string) {
	ln, err := pprofListen(setting, pprofDefaultAddr)
	if err != nil {
		slog.Error("Failed to pprof listen", "error", err)
		return
	}
	slog.Info("Serving pprof", "address", ln.Addr().String())
	if err := http.Serve(ln, nil); err != nil {
		slog.Error("pprof server stopped", "error", err)
	}
}

// pprofListen opens the listener pprof is served on: the host:port that
// setting names, or else defaultAddr, or, when another process already
// holds defaultAddr, a free port on localhost. Every gnosh on a machine
// asked for the same port, so only the first could be profiled; a gnosh
// that hung later could not be. Its port is in its log, or in
// `ss -ltnp | grep pid=<pid>`.
func pprofListen(setting, defaultAddr string) (net.Listener, error) {
	var lc net.ListenConfig
	ctx := context.Background()
	if strings.Contains(setting, ":") {
		return lc.Listen(ctx, "tcp", setting)
	}
	ln, err := lc.Listen(ctx, "tcp", defaultAddr)
	if err == nil {
		return ln, nil
	}
	return lc.Listen(ctx, "tcp", "localhost:0")
}
