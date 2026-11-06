package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/stretchr/testify/require"
)

// A project with Gno code is told, in the prompt every turn reads, that a
// plan writing Gno code audits it against the interrealm specification
// and the security guide the gno skill carries; another project is not.
// Left to the gno skill, a model that never opened it shipped three
// defects the guides name (QA, 2026-11-06).
func TestTheCoderIsAskedToAuditGnoCode(t *testing.T) {
	env := testEnv(t)
	t.Setenv("CRUSH_GLOBAL_CONFIG", filepath.Join(t.TempDir(), "crush.json"))
	crushJSON := `{
  "options": {"disable_default_providers": true, "disable_provider_auto_update": true},
  "providers": {"mock": {"id": "mock", "name": "Mock", "type": "openai",
    "base_url": "http://127.0.0.1:9/v1", "api_key": "test-key",
    "models": [{"id": "mock-model", "name": "Mock", "context_window": 8192, "default_max_tokens": 128}]}},
  "models": {"large": {"provider": "mock", "model": "mock-model"},
             "small": {"provider": "mock", "model": "mock-model"}}
}`
	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, "crush.json"), []byte(crushJSON), 0o644))
	coderPrompt := func() string {
		cfg, err := config.Init(env.workingDir, "", false, config.TrustFolderForSession())
		require.NoError(t, err)
		cfg.SetupAgents()
		c, err := NewCoordinator(t.Context(), CoordinatorOptions{
			Config:      cfg,
			Sessions:    env.sessions,
			Messages:    env.messages,
			Permissions: env.permissions,
			History:     env.history,
			FileTracker: env.filetracker,
		})
		require.NoError(t, err)
		coord := c.(*coordinator)
		require.NoError(t, coord.readyWg.Wait())
		return coord.agents[config.AgentCoder].(*sessionAgent).systemPrompt.Get()
	}

	require.NotContains(t, coderPrompt(), "<gno>", "no Gno code, no audit rule")

	require.NoError(t, os.WriteFile(filepath.Join(env.workingDir, "gnomod.toml"), []byte("module = \"gno.land/r/demo/tipjar\"\ngno = \"0.9\"\n"), 0o644))
	p := coderPrompt()
	require.Contains(t, p, "<gno>")
	require.Contains(t, p, "gnosh://skills/gno/references/gno-interrealm-v2.md")
	require.Contains(t, p, "gnosh://skills/gno/references/gno-security-guide.md")
	require.NotContains(t, p, "{{", "the section is a template, not text")
}
