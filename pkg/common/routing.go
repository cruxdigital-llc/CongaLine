package common

import (
	"encoding/json"
	"fmt"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

// RoutingConfig is the JSON structure for routing.json.
type RoutingConfig struct {
	Channels map[string]string `json:"channels"`
	Members  map[string]string `json:"members"`
}

// WebhookTarget contains the port and path for delivering channel events to a container.
type WebhookTarget struct {
	Port int    // Container-internal port for webhook delivery (0 = use agent's GatewayPort)
	Path string // HTTP path (e.g., "/slack/events" or "/webhooks/slack")
}

// WebhookTargetResolver returns the webhook target for a given agent runtime
// and channel platform. Used by GenerateRoutingJSON to construct per-runtime URLs.
// When nil, the channel's default WebhookPath() and agent's GatewayPort are used.
type WebhookTargetResolver func(agentRuntime, platform string) WebhookTarget

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
			// otherwise fall back to the channel's default path and agent's port.
			port := a.GatewayPort
			webhookPath := ch.WebhookPath()
			if resolver != nil {
				target := resolver(a.Runtime, binding.Platform)
				webhookPath = target.Path
				if target.Port != 0 {
					port = target.Port
				}
			}

			url := fmt.Sprintf("http://conga-%s:%d%s", a.Name, port, webhookPath)

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
