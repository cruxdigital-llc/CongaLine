package localprovider

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
)

// configFileForAgent returns the config file path for the given agent,
// using the runtime's config file name if available, falling back to
// checking both openclaw.json and config.yaml on disk.
func (p *LocalProvider) configFileForAgent(ctx context.Context, agentName string) string {
	dataDir := p.dataSubDir(agentName)

	// Try to resolve via the runtime
	if cfg, err := p.GetAgent(ctx, agentName); err == nil {
		if rt, err := p.runtimeForAgent(*cfg); err == nil {
			return filepath.Join(dataDir, rt.ConfigFileName())
		}
	}

	// Fallback: check which config file exists on disk
	for _, name := range []string{"config.yaml", "openclaw.json"} {
		path := filepath.Join(dataDir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Last resort
	return filepath.Join(dataDir, "openclaw.json")
}

// checkConfigIntegrity verifies the agent's config file hasn't been tampered with.
// Returns nil if hash matches or no baseline exists. Returns error on mismatch.
func (p *LocalProvider) checkConfigIntegrity(ctx context.Context, agentName string) error {
	configPath := p.configFileForAgent(ctx, agentName)
	baselinePath := filepath.Join(p.configDir(), agentName+".sha256")

	// Read current config
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	currentHash := fmt.Sprintf("%x", sha256.Sum256(data))

	// Read baseline
	baselineData, err := os.ReadFile(baselinePath)
	if err != nil {
		// No baseline — create one
		return os.WriteFile(baselinePath, []byte(currentHash), 0600)
	}

	if string(baselineData) != currentHash {
		return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s config has been modified (expected %s, got %s)",
			agentName, string(baselineData), currentHash)
	}

	return nil
}

// saveConfigBaseline stores the SHA256 hash of the current agent config file.
func (p *LocalProvider) saveConfigBaseline(ctx context.Context, agentName string) error {
	configPath := p.configFileForAgent(ctx, agentName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	baselinePath := filepath.Join(p.configDir(), agentName+".sha256")
	return os.WriteFile(baselinePath, []byte(hash), 0600)
}

// checkAgentCustomConfig validates the admin-owned include (agent-custom.json)
// does not declare Conga-owned keys. The integrity hash covers only the managed
// root, so this is the control that keeps the channel allowlist (a security
// boundary) from being extended via the include's deep-merge union. Reserved-key
// violations are reported; an unparseable (JSON5) include is left to the
// authoritative in-container check (warn-only here) to avoid false alarms on
// legit commented config — see spec §5.5 / §14.
func (p *LocalProvider) checkAgentCustomConfig(ctx context.Context, agentName string) (warn string, err error) {
	cfg, gerr := p.GetAgent(ctx, agentName)
	if gerr != nil {
		return "", nil
	}
	rt, rerr := p.runtimeForAgent(*cfg)
	if rerr != nil {
		return "", nil
	}
	fname := rt.CustomConfigFileName()
	if fname == "" {
		return "", nil
	}
	data, rerr := os.ReadFile(filepath.Join(p.dataSubDir(agentName), fname))
	if rerr != nil {
		return "", nil // absence is self-healed on next write
	}
	if verr := common.ValidateAgentCustomConfig(data); verr != nil {
		if errors.Is(verr, common.ErrCustomConfigUnparseable) {
			return fmt.Sprintf("%s could not be validated (not strict JSON); manual review advised", fname), nil
		}
		return "", fmt.Errorf("CONFIG INTEGRITY VIOLATION (%s): %w", fname, verr)
	}
	return "", nil
}

// RunIntegrityCheck checks all agent configs and logs results.
func (p *LocalProvider) RunIntegrityCheck() error {
	ctx := context.Background()
	agents, err := p.ListAgents(ctx)
	if err != nil {
		return err
	}

	logPath := filepath.Join(p.logsDir(), "integrity.log")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	now := time.Now().Format(time.RFC3339)
	for _, a := range agents {
		if err := p.checkConfigIntegrity(ctx, a.Name); err != nil {
			fmt.Fprintf(f, "%s ALERT %s: %v\n", now, a.Name, err)
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
			continue
		}
		// Managed root is intact; now guard the admin-owned include.
		if warn, err := p.checkAgentCustomConfig(ctx, a.Name); err != nil {
			fmt.Fprintf(f, "%s ALERT %s: %v\n", now, a.Name, err)
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
		} else if warn != "" {
			fmt.Fprintf(f, "%s WARN %s: %s\n", now, a.Name, warn)
			fmt.Fprintf(os.Stderr, "WARN: %s: %s\n", a.Name, warn)
		} else {
			fmt.Fprintf(f, "%s OK %s: config integrity verified\n", now, a.Name)
		}
	}

	return nil
}
