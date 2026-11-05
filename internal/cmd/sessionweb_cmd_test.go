package cmd

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/charmbracelet/crush/internal/artifacts"
)

// `gnosh session web` builds the app before it listens, and the app
// starts the artifact server at its default address. The page's default
// port must still be free once that server holds its own.
func TestSessionWebDefaultPortIsNotTheArtifactServers(t *testing.T) {
	s := artifacts.Start("", t.TempDir())
	require.NotNil(t, s, "the artifact server did not start")
	t.Cleanup(func() { _ = s.Close() })

	port := sessionWebCmd.Flags().Lookup("port").DefValue
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", net.JoinHostPort("127.0.0.1", port))
	require.NoError(t, err, "session web cannot listen on its default port")
	require.NoError(t, ln.Close())
}

// A default port another page holds falls back to a free one; a port asked
// for is that port or nothing.
func TestSessionWebFallsBackFromATakenDefaultPort(t *testing.T) {
	t.Parallel()
	taken, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = taken.Close() })
	port := taken.Addr().(*net.TCPAddr).Port

	ln, err := listenSessionWeb(t.Context(), port, false)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	got := ln.Addr().(*net.TCPAddr)
	require.NotEqual(t, port, got.Port)
	require.True(t, got.IP.IsLoopback(), "the fallback stays on this machine: %s", got)

	_, err = listenSessionWeb(t.Context(), port, true)
	require.Error(t, err, "a port asked for is not swapped for another")
}
