package openclaw

import (
	"strings"

	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// ContainerPort is the gateway port inside OpenClaw containers.
const ContainerPort = 18789

func (r *Runtime) ContainerSpec(agent provider.AgentConfig) runtime.ContainerSpec {
	return runtime.ContainerSpec{
		ContainerPort: ContainerPort,
		User:          "1000:1000",
		Memory:        "2g",
		CPUs:          "0.75",
		PIDsLimit:     "256",
		EnvVars:       map[string]string{"NODE_OPTIONS": "--max-old-space-size=1536"},
	}
}

func (r *Runtime) DefaultImage() string {
	return "ghcr.io/openclaw/openclaw:latest"
}

func (r *Runtime) ContainerDataPath() string {
	return "/home/node/.openclaw"
}

func (r *Runtime) WorkspacePath() string {
	return "data/workspace"
}

func (r *Runtime) SupportsNodeProxy() bool { return true }

// PluginsToInstall returns the external OpenClaw plugin packages required
// for the agent's configured channels. Starting with v2026.5.x, Slack is
// shipped as an external plugin (@openclaw/slack) rather than bundled in
// the image, so any agent with a slack channel binding needs it installed
// into the data dir before the gateway starts.
//
// Platform matching is case- and whitespace-insensitive so a hand-edited
// agent JSON with "Slack" or " slack " still triggers the install. The
// canonical platform name registered in pkg/channels/slack is "slack".
func (r *Runtime) PluginsToInstall(agent provider.AgentConfig) []string {
	var plugins []string
	for _, ch := range agent.Channels {
		if strings.ToLower(strings.TrimSpace(ch.Platform)) == "slack" {
			plugins = append(plugins, "@openclaw/slack")
			break
		}
	}
	return plugins
}

// PluginInstallCommand returns the in-container command that installs an
// OpenClaw plugin into ~/.openclaw/npm. The install errors out with exit 1
// if the plugin is already on disk ("plugin already exists; delete it
// first"), which providers tolerate via best-effort wrapping — the
// already-installed plugin is the desired end state. Use `--force` when
// an explicit reinstall is needed (out of scope for the auto-bootstrap).
//
// NB: `--yes` is NOT a valid flag on OpenClaw v2026.5.18+ — passing it
// makes the command exit non-zero before any work happens. Keep the
// command minimal.
func (r *Runtime) PluginInstallCommand(spec string) []string {
	return []string{"openclaw", "plugins", "install", spec}
}
