package remoteprovider

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	posixpath "path"
	"strings"
	"time"

	"github.com/cruxdigital-llc/conga-line/pkg/common"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

// checkConfigIntegrity verifies openclaw.json hasn't been tampered with on the remote host.
func (p *RemoteProvider) checkConfigIntegrity(agentName string) error {
	configPath := posixpath.Join(p.remoteDataSubDir(agentName), "openclaw.json")
	baselinePath := posixpath.Join(p.remoteConfigDir(), agentName+".sha256")

	data, err := p.ssh.Download(configPath)
	if err != nil {
		return fmt.Errorf("config not found: %w", err)
	}

	currentHash := fmt.Sprintf("%x", sha256.Sum256(data))

	baselineData, err := p.ssh.Download(baselinePath)
	if err != nil {
		// No baseline — create one
		return p.ssh.Upload(baselinePath, []byte(currentHash), 0600)
	}

	if string(baselineData) != currentHash {
		return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s/openclaw.json has been modified (expected %s, got %s)",
			agentName, string(baselineData), currentHash)
	}

	return nil
}

// saveConfigBaseline stores the SHA256 hash of the current openclaw.json on the
// remote host, plus baselines for the Conga-managed include layers (feature #31).
func (p *RemoteProvider) saveConfigBaseline(agentName string) error {
	configPath := posixpath.Join(p.remoteDataSubDir(agentName), "openclaw.json")
	data, err := p.ssh.Download(configPath)
	if err != nil {
		return err
	}

	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	baselinePath := posixpath.Join(p.remoteConfigDir(), agentName+".sha256")
	if err := p.ssh.Upload(baselinePath, []byte(hash), 0600); err != nil {
		return err
	}
	if a, gerr := p.GetAgent(context.Background(), agentName); gerr == nil {
		return p.saveManagedIncludeBaselines(*a)
	}
	return nil
}

// managedIncludeFiles / includeFilesForGuard resolve the $include layers for an
// agent's runtime: the Conga-managed layers (hash-verified) and, for the guard,
// the admin-owned layer too (reserved-key guarded only). nil for runtimes
// without $include layering (Hermes).
func managedIncludeFiles(a provider.AgentConfig) []string {
	rt, err := runtime.Get(runtime.ResolveRuntime(a.Runtime, ""))
	if err != nil {
		return nil
	}
	return rt.ManagedCustomConfigFiles()
}

func includeFilesForGuard(a provider.AgentConfig) []string {
	rt, err := runtime.Get(runtime.ResolveRuntime(a.Runtime, ""))
	if err != nil {
		return nil
	}
	files := append([]string{}, rt.ManagedCustomConfigFiles()...)
	if admin := rt.CustomConfigFileName(); admin != "" {
		files = append(files, admin)
	}
	return files
}

func (p *RemoteProvider) managedIncludeBaselinePath(agentName, fname string) string {
	return posixpath.Join(p.remoteConfigDir(), agentName+"."+fname+".sha256")
}

// saveManagedIncludeBaselines records the deployed-baseline hashes of the managed
// include layers on the remote host.
func (p *RemoteProvider) saveManagedIncludeBaselines(a provider.AgentConfig) error {
	for _, fname := range managedIncludeFiles(a) {
		data, err := p.ssh.Download(posixpath.Join(p.remoteDataSubDir(a.Name), fname))
		if err != nil {
			continue // absence self-heals on next write
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		if err := p.ssh.Upload(p.managedIncludeBaselinePath(a.Name, fname), []byte(hash), 0600); err != nil {
			return err
		}
	}
	return nil
}

// checkManagedIncludeIntegrity verifies the Conga-managed include layers match
// their deployed baseline on the remote host. Missing baseline self-heals.
func (p *RemoteProvider) checkManagedIncludeIntegrity(a provider.AgentConfig) error {
	for _, fname := range managedIncludeFiles(a) {
		data, err := p.ssh.Download(posixpath.Join(p.remoteDataSubDir(a.Name), fname))
		if err != nil {
			continue // absence self-heals on next write
		}
		current := fmt.Sprintf("%x", sha256.Sum256(data))
		bp := p.managedIncludeBaselinePath(a.Name, fname)
		baseline, derr := p.ssh.Download(bp)
		if derr != nil {
			if uerr := p.ssh.Upload(bp, []byte(current), 0600); uerr != nil {
				return uerr
			}
			continue
		}
		if string(baseline) != current {
			return fmt.Errorf("CONFIG INTEGRITY VIOLATION: %s/%s has been modified (expected %s, got %s)",
				a.Name, fname, string(baseline), current)
		}
	}
	return nil
}

// checkIncludeReservedKeys validates that NO $include layer on the remote host
// declares Conga-owned keys (esp. the channel allowlist — a security boundary the
// integrity hash of the managed root cannot cover, since OpenClaw unions include
// objects). Covers admin + managed layers (feature #31). See spec §5.5.
func (p *RemoteProvider) checkIncludeReservedKeys(a provider.AgentConfig) (warns []string, err error) {
	for _, fname := range includeFilesForGuard(a) {
		data, derr := p.ssh.Download(posixpath.Join(p.remoteDataSubDir(a.Name), fname))
		if derr != nil {
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

// RunIntegrityCheck checks all agent configs on the remote host and logs results.
func (p *RemoteProvider) RunIntegrityCheck() error {
	agents, err := p.ListAgents(context.Background())
	if err != nil {
		return err
	}

	logPath := posixpath.Join(p.remoteDir, "logs", "integrity.log")
	now := time.Now().Format(time.RFC3339)

	var logLines []string
	for _, a := range agents {
		if err := p.checkConfigIntegrity(a.Name); err != nil {
			logLines = append(logLines, fmt.Sprintf("%s ALERT %s: %v", now, a.Name, err))
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
			continue
		}
		// Verify the Conga-managed include layers against their deployed baseline.
		if err := p.checkManagedIncludeIntegrity(a); err != nil {
			logLines = append(logLines, fmt.Sprintf("%s ALERT %s: %v", now, a.Name, err))
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
			continue
		}
		// Guard every include layer (admin + managed) against reserved keys.
		warns, err := p.checkIncludeReservedKeys(a)
		if err != nil {
			logLines = append(logLines, fmt.Sprintf("%s ALERT %s: %v", now, a.Name, err))
			fmt.Fprintf(os.Stderr, "ALERT: %v\n", err)
		} else if len(warns) > 0 {
			for _, warn := range warns {
				logLines = append(logLines, fmt.Sprintf("%s WARN %s: %s", now, a.Name, warn))
				fmt.Fprintf(os.Stderr, "WARN: %s: %s\n", a.Name, warn)
			}
		} else {
			logLines = append(logLines, fmt.Sprintf("%s OK %s: config integrity verified", now, a.Name))
		}
	}

	// Append to remote log via stdin pipe (avoids shell interpretation of log content)
	if len(logLines) > 0 {
		content := strings.Join(logLines, "\n") + "\n"
		session, err := p.ssh.session()
		if err == nil {
			session.Stdin = strings.NewReader(content)
			session.Run(fmt.Sprintf("cat >> %s", shellQuote(logPath)))
			session.Close()
		}
	}

	return nil
}
