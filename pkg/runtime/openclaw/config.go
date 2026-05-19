package openclaw

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

//go:embed openclaw-defaults.json
var openclawDefaults []byte

func (r *Runtime) GenerateConfig(params runtime.ConfigParams) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(openclawDefaults, &config); err != nil {
		return nil, fmt.Errorf("failed to parse openclaw-defaults.json: %w", err)
	}

	config["gateway"] = buildGatewayConfig(ContainerPort, params.Agent.GatewayPort, params.GatewayToken)

	channelsCfg := map[string]any{}
	pluginsCfg := map[string]any{}

	// Group bindings by platform so channels that support multi-binding
	// (e.g. a team agent on several Slack channels) get one merged config
	// section per platform instead of the last-binding-wins behavior of
	// iterating each binding independently.
	byPlatform := map[string][]channels.ChannelBinding{}
	for _, binding := range params.Agent.Channels {
		byPlatform[binding.Platform] = append(byPlatform[binding.Platform], binding)
	}

	// Iterate in sorted order for deterministic output — platforms go into
	// the openclaw.json channels/plugins maps, and while json.MarshalIndent
	// sorts map keys anyway, sorting here avoids nondeterminism in any
	// intermediate observation (e.g. tests that inspect channelsCfg directly).
	platforms := make([]string, 0, len(byPlatform))
	for p := range byPlatform {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)

	for _, platform := range platforms {
		bindings := byPlatform[platform]
		ch, ok := channels.Get(platform)
		if !ok {
			continue
		}
		hasCreds := ch.HasCredentials(params.Secrets.Values)
		pluginsCfg[platform] = ch.OpenClawPluginConfig(hasCreds)
		if !hasCreds {
			continue
		}

		var section map[string]any
		var err error
		if multi, ok := ch.(channels.MultiBindingChannel); ok {
			section, err = multi.OpenClawChannelConfigMulti(string(params.Agent.Type), bindings, params.Secrets.Values)
		} else {
			// Channels that don't opt into multi-binding use only the first
			// binding (legacy behavior). The bind guard already rejects
			// extra same-platform bindings for non-multi channels.
			section, err = ch.OpenClawChannelConfig(string(params.Agent.Type), bindings[0], params.Secrets.Values)
		}
		if err != nil {
			return nil, fmt.Errorf("channel %s config: %w", platform, err)
		}
		if section != nil {
			channelsCfg[platform] = section
		}
	}

	if len(channelsCfg) > 0 {
		config["channels"] = channelsCfg
	}
	if len(pluginsCfg) > 0 {
		config["plugins"] = map[string]any{"entries": pluginsCfg}
	}

	return json.MarshalIndent(config, "", "  ")
}

func (r *Runtime) ConfigFileName() string { return "openclaw.json" }

// buildGatewayConfig produces the gateway section of openclaw.json.
func buildGatewayConfig(containerPort, hostPort int, token string) map[string]any {
	origins := []string{
		fmt.Sprintf("http://localhost:%d", containerPort),
		fmt.Sprintf("http://127.0.0.1:%d", containerPort),
	}
	if hostPort != containerPort {
		origins = append(origins,
			fmt.Sprintf("http://localhost:%d", hostPort),
			fmt.Sprintf("http://127.0.0.1:%d", hostPort),
		)
	}

	gw := map[string]any{
		"port": containerPort,
		"mode": "remote",
		"bind": "lan",
		"remote": map[string]any{
			"url": fmt.Sprintf("http://localhost:%d", containerPort),
		},
		"controlUi": map[string]any{
			"allowedOrigins": origins,
		},
	}

	if token != "" {
		gw["auth"] = map[string]any{
			"mode":  "token",
			"token": token,
		}
	}

	return gw
}
