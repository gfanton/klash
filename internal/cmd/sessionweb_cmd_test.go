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
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", port))
	require.NoError(t, err, "session web cannot listen on its default port")
	require.NoError(t, ln.Close())
}
