package common

import (
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// RoutingConfig is the JSON structure for routing.json.
type RoutingConfig struct {
	Channels map[string]string `json:"channels"`
	Members  map[string]string `json:"members"`
}

// WebhookTarget contains the port and path for delivering channel events to a container.
type WebhookTarget struct {
	Port int    // Container-internal port for webhook delivery (0 = use BaseGatewayPort, the container-internal gateway port)
	Path string // HTTP path (e.g., "/slack/events" or "/webhooks/slack")

	// Loopback selects the host-networking router topology: deliver to
	// 127.0.0.1:<agent host GatewayPort> instead of the Docker-network name
	// conga-<agent>:<port>. The router runs with --network host (no per-agent
	// bridge attach), so it reaches each agent through its published loopback
	// port. When set, Port is ignored — GenerateRoutingJSON substitutes the
	// agent's host GatewayPort. Valid only for runtimes whose webhook endpoint
	// is the gateway port (OpenClaw, WebhookPort()==0); runtimes with a separate
	// unpublished webhook port (Hermes:8644) are not yet reachable this way.
	Loopback bool
}

// WebhookTargetResolver returns the webhook target for a given agent runtime
// and channel platform. Used by GenerateRoutingJSON to construct per-runtime URLs.
// When nil, the channel's default WebhookPath() and the container-internal
// BaseGatewayPort are used.
type WebhookTargetResolver func(agentRuntime, platform string) WebhookTarget

// LoopbackWebhookResolver returns a resolver that selects the host-networking
// router topology (see WebhookTarget.Loopback). The HTTP path stays
// runtime-aware (so OpenClaw gets the channel's default path), but delivery is
// to the agent's published 127.0.0.1:<hostPort> rather than the Docker-network
// container name. globalDefaultRuntime is the provider's configured default
// runtime, used when an agent's Runtime is unset (pass "" to fall back to
// OpenClaw). This is the standard router topology for all single-host providers
// (AWS, local, remote).
func LoopbackWebhookResolver(globalDefaultRuntime string) WebhookTargetResolver {
	return func(agentRuntime, platform string) WebhookTarget {
		name := runtime.ResolveRuntime(agentRuntime, globalDefaultRuntime)
		if rt, err := runtime.Get(name); err == nil {
			return WebhookTarget{Path: rt.WebhookPath(platform), Loopback: true}
		}
		if ch, ok := channels.Get(platform); ok {
			return WebhookTarget{Path: ch.WebhookPath(), Loopback: true}
		}
		return WebhookTarget{Path: "/" + platform + "/events", Loopback: true}
	}
}

// GenerateRoutingJSON builds routing.json from a list of agents.
// The resolver maps (runtime, platform) → webhook path so that different
// runtimes receive events at their expected endpoints.
// Pass nil for resolver to use each channel's default webhook path.
func GenerateRoutingJSON(agents []provider.AgentConfig, resolver WebhookTargetResolver) ([]byte, error) {
	cfg := RoutingConfig{
		Channels: make(map[string]string),
		Members:  make(map[string]string),
	}

	for _, a := range agents {
		if a.Paused {
			continue
		}
		for _, binding := range a.Channels {
			ch, ok := channels.Get(binding.Platform)
			if !ok {
				continue
			}

			// Resolve the webhook target: runtime-specific if resolver provided,
			// otherwise fall back to the channel's default path and the
			// container-internal gateway port.
			//
			// Two delivery topologies:
			//   - Bridge (default): the router is attached to the agent's Docker
			//     network and reaches it by hostname conga-<agent> on the
			//     container-internal gateway port (BaseGatewayPort) — NOT the
			//     agent's host-side GatewayPort.
			//   - Loopback (target.Loopback): the router runs --network host and
			//     reaches the agent through its published 127.0.0.1:<hostPort>,
			//     where <hostPort> is the agent's host-side GatewayPort. This is
			//     the standard topology for all single-host providers; it removes
			//     the brittle per-agent bridge attach.
			host := "conga-" + a.Name
			port := BaseGatewayPort
			webhookPath := ch.WebhookPath()
			if resolver != nil {
				target := resolver(a.Runtime, binding.Platform)
				webhookPath = target.Path
				if target.Loopback {
					host = "127.0.0.1"
					port = a.GatewayPort
				} else if target.Port != 0 {
					port = target.Port
				}
			}

			url := fmt.Sprintf("http://%s:%d%s", host, port, webhookPath)

			switch string(a.Type) {
			case "user":
				if binding.ID != "" {
					cfg.Members[binding.ID] = url
				}
			case "team":
				if binding.ID != "" {
					cfg.Channels[binding.ID] = url
				}
			}
		}
	}

	return json.MarshalIndent(cfg, "", "  ")
}
