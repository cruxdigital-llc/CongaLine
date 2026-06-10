package localprovider

import (
	"context"
	"crypto/sha256"
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

// saveConfigBaseline stores the SHA256 hash of the current agent config file,
// plus baselines for the Conga-managed include layers (feature #31).
func (p *LocalProvider) saveConfigBaseline(ctx context.Context, agentName string) error {
	configPath := p.configFileForAgent(ctx, agentName)
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	baselinePath := filepath.Join(p.configDir(), agentName+".sha256")
	if err := os.WriteFile(baselinePath, []byte(hash), 0600); err != nil {
		return err
	}
	return p.saveManagedIncludeBaselines(ctx, agentName)
}

// managedIncludeFiles returns the Conga-deployed managed include layers for an
// agent's runtime (fleet-custom.json, agent-managed-custom.json), or nil if the
// runtime has no $include layering. These are hash-verified; the admin-owned
// agent-custom.json deliberately is not.
func (p *LocalProvider) managedIncludeFiles(ctx context.Context, agentName string) []string {
	cfg, err := p.GetAgent(ctx, agentName)
	if err != nil {
		return nil
	}
	rt, err := p.runtimeForAgent(*cfg)
	if err != nil {
		return nil
	}
	return rt.ManagedCustomConfigFiles()
}

// includeFilesForGuard returns every $include layer that gets the reserved-key
// guard: the admin-owned file plus the Conga-managed layers.
func (p *LocalProvider) includeFilesForGuard(ctx context.Context, agentName string) []string {
	files := p.managedIncludeFiles(ctx, agentName)
	if cfg, err := p.GetAgent(ctx, agentName); err == nil {
		if rt, err := p.runtimeForAgent(*cfg); err == nil {
			if admin := rt.CustomConfigFileName(); admin != "" {
				files = append(files, admin)
			}
		}
	}
	return files
}

// managedIncludeBaselinePath is the per-file hash baseline for a managed include.
func (p *LocalProvider) managedIncludeBaselinePath(agentName, fname string) string {
	return filepath.Join(p.configDir(), agentName+"."+fname+".sha256")
}

// saveManagedIncludeBaselines records the deployed-baseline hashes of the managed
// include layers so checkManagedIncludeIntegrity can detect on-host tampering.
func (p *LocalProvider) saveManagedIncludeBaselines(ctx context.Context, agentName string) error {
	for _, fname := range p.managedIncludeFiles(ctx, agentName) {
		data, err := os.ReadFile(filepath.Join(p.dataSubDir(agentName), fname))
		if err != nil {
			continue // absence self-heals on next write
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		if err := os.WriteFile(p.managedIncludeBaselinePath(agentName, fname), []byte(hash), 0600); err != nil {
			return err
		}
	}
	return nil
}

// checkManagedIncludeIntegrity verifies the Conga-managed include layers match
// their deployed baseline (detecting on-host tampering of Conga-owned config).
// A missing baseline self-heals. The admin-owned agent-custom.json is excluded —
// admin drift is expected there and guarded only by the reserved-key check.
func (p *LocalProvider) checkManagedIncludeIntegrity(ctx context.Context, agentName string) error {
	for _, fname := range p.managedIncludeFiles(ctx, agentName) {
		data, err := os.ReadFile(filepath.Join(p.dataSubDir(agentName), fname))
		if err != nil {
			continue // absence self-heals on next write
		}
		current := fmt.Sprintf("%x", sha256.Sum256(data))
		bp := p.managedIncludeBaselinePath(agentName, fname)
		baseline, err := os.ReadFile(bp)
		if err != nil {
			if werr := os.WriteFile(bp, []byte(current), 0600); werr != nil {
				return werr
			}
			continue
		}
		if string(baseline) != current {
			return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s/%s has been modified (expected %s, got %s)",
				agentName, fname, string(baseline), current)
		}
	}
	return nil
}

// checkIncludeReservedKeys validates that NO $include layer declares Conga-owned
// keys — the admin-owned agent-custom.json plus the Conga-managed fleet-custom /
// agent-managed-custom layers (feature #31). The integrity hash covers only the
// managed root, so this is the control that keeps the channel allowlist (a
// security boundary) from being extended via an include's deep-merge union, in
// any layer. Reserved-key violations are hard errors; an unparseable (JSON5)
// include is left to the authoritative in-container check (warn-only here) to
// avoid false alarms on legit commented config — see spec §5.5 / §14.
func (p *LocalProvider) checkIncludeReservedKeys(ctx context.Context, agentName string) (warns []string, err error) {
	for _, fname := range p.includeFilesForGuard(ctx, agentName) {
		data, rerr := os.ReadFile(filepath.Join(p.dataSubDir(agentName), fname))
		if rerr != nil {
			continue // absence is self-healed on next write
		}
		warn, verr := common.ClassifyIncludeValidation(fname, data)
		if verr != nil {
			return warns, verr
		}
		if warn != "" {
			warns = append(warns, warn)
		}
	}
	return warns, nil
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
		// Managed root is intact; verify the Conga-managed include layers
		// against their deployed baseline (tamper detection).
		if err := p.checkManagedIncludeIntegrity(ctx, a.Name); err != nil {
			fmt.Fprintf(f, "%s ALERT %s: %v\n", now, a.Name, err)
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
			continue
		}
		// Guard every include layer (admin + managed) against reserved keys.
		warns, err := p.checkIncludeReservedKeys(ctx, a.Name)
		if err != nil {
			fmt.Fprintf(f, "%s ALERT %s: %v\n", now, a.Name, err)
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
		} else if len(warns) > 0 {
			for _, warn := range warns {
				fmt.Fprintf(f, "%s WARN %s: %s\n", now, a.Name, warn)
				fmt.Fprintf(os.Stderr, "WARN: %s: %s\n", a.Name, warn)
			}
		} else {
			fmt.Fprintf(f, "%s OK %s: config integrity verified\n", now, a.Name)
		}
	}

	return nil
}
