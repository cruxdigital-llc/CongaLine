# Spec: Portable Egress Policy Compliance

## Overview

Fix all three providers to consistently respect the `mode` field in `conga-policy.yaml`'s egress section, and add iptables enforcement to the AWS bootstrap. Currently only the local provider honors `mode: validate` vs `mode: enforce`. The remote provider ignores `mode` and always enforces, the AWS provider has no mode check, and the AWS bootstrap lacks iptables DROP rules entirely (relying solely on the Envoy proxy).

This spec aligns all three providers: when `mode` is `enforce` (or absent — the default), start the egress proxy and apply iptables DROP rules. When `mode` is `validate`, log warnings but do not enforce.

**Default mode is `enforce`** — security-first. Operators who want warn-only must explicitly set `mode: validate`.

**Source**: Policy schema spec (Feature 12), security standards principle 7 ("Policy is portable, enforcement is tiered").

---

## Phase 1: Default Mode Change

### 1.1 Update policy schema default

The `mode` field currently defaults to `validate` when empty/absent. Change to default to `enforce`.

**File**: `cli/internal/policy/policy.go`

In `Validate()` or after loading, normalize empty mode to `enforce`:

```go
// In Load() or MergeForAgent(), after loading:
if pf.Egress != nil && pf.Egress.Mode == "" {
    pf.Egress.Mode = "enforce"
}
```

This must also apply to agent overrides — if an agent override has an egress section with no mode, it inherits the global mode (already handled by `MergeForAgent()` shallow merge, since the entire egress section is replaced).

### 1.2 Update validation

**File**: `cli/internal/policy/policy.go`

The `validateEgress()` function accepts `""` as valid. Keep this — the normalization in 1.1 means empty mode is resolved to `enforce` before reaching provider code.

---

## Phase 2: Remote Provider — Respect `mode` field

### 2.1 `ProvisionAgent` (line ~236)

**File**: `cli/internal/provider/remoteprovider/provider.go`

Replace:
```go
egressEnforce := false
if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
    egressEnforce = true // Remote always enforces when domains defined
}
```

With:
```go
egressEnforce := false
if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
    if egressPolicy.Mode != "enforce" {
        fmt.Fprintf(os.Stderr, "Warning: Egress rules defined but not enforced in validate mode. Set mode: enforce in conga-policy.yaml to activate the egress proxy.\n")
    } else {
        egressEnforce = true
    }
}
```

This matches the local provider's pattern exactly (see `localprovider/provider.go` lines 192-198).

### 2.2 `RefreshAgent` (line ~599)

**File**: `cli/internal/provider/remoteprovider/provider.go`

Same replacement — the `RefreshAgent` function has an identical block at line ~599:

Replace:
```go
egressEnforce := false
if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
    egressEnforce = true
}
```

With:
```go
egressEnforce := false
if egressPolicy != nil && len(egressPolicy.AllowedDomains) > 0 {
    if egressPolicy.Mode != "enforce" {
        fmt.Fprintf(os.Stderr, "Warning: Egress rules defined but not enforced in validate mode. Set mode: enforce in conga-policy.yaml to activate the egress proxy.\n")
    } else {
        egressEnforce = true
    }
}
```

### 2.3 `ensureEgressIptables` (line ~437)

**File**: `cli/internal/provider/remoteprovider/provider.go`

Update the comment and add the mode check. Replace:
```go
// ensureEgressIptables checks if iptables egress rules are in place for a running
// container and re-applies them if missing. Handles IP changes after container restart.
//
// Unlike the local provider, this does NOT check egressPolicy.Mode because the remote
// provider always enforces iptables when allowed_domains are defined. The "validate" vs
// "enforce" mode distinction only applies to the local provider.
func (p *RemoteProvider) ensureEgressIptables(ctx context.Context, agentName string) {
	egressPolicy, err := policy.LoadEgressPolicy(p.dataDir, agentName)
	if err != nil || egressPolicy == nil || len(egressPolicy.AllowedDomains) == 0 {
		return
	}
```

With:
```go
// ensureEgressIptables checks if iptables egress rules are in place for a running
// container and re-applies them if missing. Handles IP changes after container restart.
// Only applies when egress policy mode is "enforce".
func (p *RemoteProvider) ensureEgressIptables(ctx context.Context, agentName string) {
	egressPolicy, err := policy.LoadEgressPolicy(p.dataDir, agentName)
	if err != nil || egressPolicy == nil || egressPolicy.Mode != "enforce" || len(egressPolicy.AllowedDomains) == 0 {
		return
	}
```

This matches the local provider's `ensureEgressIptables` check (see `localprovider/provider.go` line 392).

---

## Phase 3: AWS Bootstrap — Respect `mode` field + iptables enforcement

### 3.1 Parse `mode` in `generate_egress_conf()`

**File**: `terraform/user-data.sh.tftpl`

Add mode detection to the YAML parser. After the existing domain parsing variables (line ~450), add:

```bash
local GLOBAL_MODE=""
local AGENT_MODE=""
```

In the global egress parsing section, add mode detection:
```bash
# Detect global egress mode
if $IN_GLOBAL_EGRESS && echo "$line" | grep -qE "^  mode:"; then
    GLOBAL_MODE=$(echo "$line" | sed 's/^  mode: *//' | tr -d '"' | tr -d "'" | xargs)
fi
```

In the agent egress parsing section, add mode detection:
```bash
# Detect agent egress mode
if $IN_AGENT_EGRESS && echo "$line" | grep -qE "^      mode:"; then
    AGENT_MODE=$(echo "$line" | sed 's/^      mode: *//' | tr -d '"' | tr -d "'" | xargs)
fi
```

After the agent override merge section (line ~563), add mode resolution:
```bash
# Resolve effective mode (agent override takes precedence, default is "enforce")
local EFFECTIVE_MODE=""
if [ -n "$AGENT_ALLOWED" ]; then
    EFFECTIVE_MODE="${AGENT_MODE:-$GLOBAL_MODE}"
else
    EFFECTIVE_MODE="$GLOBAL_MODE"
fi
EFFECTIVE_MODE="${EFFECTIVE_MODE:-enforce}"
```

### 3.2 Skip config generation when `mode != enforce`

At the end of `generate_egress_conf()`, before the "No domains = no egress conf" check (line ~585), add:

```bash
# Validate mode — only generate config when enforcing
if [ "$EFFECTIVE_MODE" != "enforce" ]; then
    if [ -n "$DOMAINS" ]; then
        log "Egress rules defined for $AGENT_NAME but mode=$EFFECTIVE_MODE — proxy not activated. Set mode: enforce to activate."
    fi
    return
fi
```

### 3.3 Verify proxy startup is gated

The existing proxy startup code (lines 711-743) is already gated on `[ -f "$EGRESS_CONF" ]`. Since `generate_egress_conf()` will no longer create the config file in validate mode, the proxy will not start. No changes needed to the proxy startup section.

### 3.4 Add iptables DROP rules to bootstrap

**File**: `terraform/user-data.sh.tftpl`

After the proxy startup section (line ~743) and before the systemd unit generation, add iptables enforcement when the egress proxy is active:

```bash
  # Apply iptables egress DROP rules (when proxy is active)
  if [ -f "$EGRESS_CONF" ]; then
    # Get agent container IP and network subnet for iptables rules.
    # Container must be running — start it temporarily if needed to get the IP.
    # The systemd unit will manage the actual lifecycle.
    AGENT_NET="conga-$AGENT_NAME"
    AGENT_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"$AGENT_NET\").IPAddress}}" "conga-$AGENT_NAME" 2>/dev/null || echo "")
    NET_CIDR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' "$AGENT_NET" 2>/dev/null || echo "")
    if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then
      # Idempotent insertion: check-or-insert pattern
      # Rule order (iptables -I pushes to top, so insert in reverse):
      #   1. ESTABLISHED,RELATED → RETURN (allow response traffic)
      #   2. dst=subnet → RETURN (allow proxy + Docker DNS)
      #   3. DROP (block everything else from this source)
      iptables -C DOCKER-USER -s "$AGENT_IP" -j DROP 2>/dev/null || \
        iptables -I DOCKER-USER -s "$AGENT_IP" -j DROP
      iptables -C DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN 2>/dev/null || \
        iptables -I DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN
      iptables -C DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || \
        iptables -I DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
      log "Egress iptables: DROP rules applied for conga-$AGENT_NAME ($AGENT_IP)"
    else
      log "WARNING: Could not determine container IP or subnet for iptables egress rules"
    fi
  fi
```

This mirrors the exact same iptables rule structure used by the shared `iptables.AddRulesCmd()` in `cli/internal/provider/iptables/rules.go` (lines 21-35).

### 3.5 Add iptables cleanup to systemd unit

In the systemd unit template (line ~752), add `ExecStopPost` to clean up iptables rules when the container stops, and `ExecStartPost` to re-apply them when it starts:

After the existing `ExecStartPost` line (router reconnect), add:
```bash
ExecStartPost=-/bin/bash -c 'AGENT_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"conga-$AGENT_NAME\").IPAddress}}" conga-$AGENT_NAME 2>/dev/null); NET_CIDR=$(docker network inspect -f "{{range .IPAM.Config}}{{.Subnet}}{{end}}" conga-$AGENT_NAME 2>/dev/null); if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ] && [ -f "/opt/conga/config/egress-$AGENT_NAME.yaml" ]; then iptables -C DOCKER-USER -s $AGENT_IP -j DROP 2>/dev/null || iptables -I DOCKER-USER -s $AGENT_IP -j DROP; iptables -C DOCKER-USER -s $AGENT_IP -d $NET_CIDR -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s $AGENT_IP -d $NET_CIDR -j RETURN; iptables -C DOCKER-USER -s $AGENT_IP -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s $AGENT_IP -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN; fi'
```

Add before `ExecStop`:
```bash
ExecStopPost=-/bin/bash -c 'AGENT_IP=$(docker inspect -f "{{(index .NetworkSettings.Networks \"conga-$AGENT_NAME\").IPAddress}}" conga-$AGENT_NAME 2>/dev/null); NET_CIDR=$(docker network inspect -f "{{range .IPAM.Config}}{{.Subnet}}{{end}}" conga-$AGENT_NAME 2>/dev/null); if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then iptables -D DOCKER-USER -s $AGENT_IP -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || true; iptables -D DOCKER-USER -s $AGENT_IP -d $NET_CIDR -j RETURN 2>/dev/null || true; iptables -D DOCKER-USER -s $AGENT_IP -j DROP 2>/dev/null || true; fi'
```

### 3.6 Add iptables to refresh script

**File**: `cli/scripts/refresh-user.sh.tmpl`

After the `systemctl restart` and router reconnect, add iptables re-application:

```bash
# Re-apply egress iptables rules (if egress proxy config exists)
EGRESS_CONF="/opt/conga/config/egress-$AGENT_NAME.yaml"
if [ -f "$EGRESS_CONF" ]; then
  # Wait for container to be running
  for i in $(seq 1 10); do
    AGENT_IP=$(docker inspect -f '{{(index .NetworkSettings.Networks "conga-'$AGENT_NAME'").IPAddress}}' conga-$AGENT_NAME 2>/dev/null || echo "")
    [ -n "$AGENT_IP" ] && break
    sleep 1
  done
  NET_CIDR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' conga-$AGENT_NAME 2>/dev/null || echo "")
  if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then
    iptables -C DOCKER-USER -s "$AGENT_IP" -j DROP 2>/dev/null || \
      iptables -I DOCKER-USER -s "$AGENT_IP" -j DROP
    iptables -C DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN 2>/dev/null || \
      iptables -I DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN
    iptables -C DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || \
      iptables -I DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN
    echo "Egress iptables: DROP rules applied for conga-$AGENT_NAME ($AGENT_IP)"
  else
    echo "WARNING: Could not apply egress iptables rules — container IP or subnet not found"
  fi
fi
```

### 3.7 Add iptables cleanup on agent removal

**File**: `cli/scripts/refresh-all.sh.tmpl` (if it exists) and in the bootstrap cleanup section

When stopping/removing an agent, clean up its iptables rules before stopping the container:

```bash
# Clean up egress iptables rules before stopping
AGENT_IP=$(docker inspect -f '{{(index .NetworkSettings.Networks "conga-'$UNIT_NAME'").IPAddress}}' "$UNIT_NAME" 2>/dev/null || echo "")
NET_CIDR=$(docker network inspect -f '{{range .IPAM.Config}}{{.Subnet}}{{end}}' "conga-${UNIT_NAME#conga-}" 2>/dev/null || echo "")
if [ -n "$AGENT_IP" ] && [ -n "$NET_CIDR" ]; then
  iptables -D DOCKER-USER -s "$AGENT_IP" -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || true
  iptables -D DOCKER-USER -s "$AGENT_IP" -d "$NET_CIDR" -j RETURN 2>/dev/null || true
  iptables -D DOCKER-USER -s "$AGENT_IP" -j DROP 2>/dev/null || true
fi
```

---

## Phase 4: Enforcement Report — Reflect actual behavior

### 4.1 Update `egressReport()`

**File**: `cli/internal/policy/enforcement.go`

Replace the current `egressReport` function (lines 40-74):

```go
func egressReport(e *EgressPolicy, providerName string) []RuleReport {
	var reports []RuleReport

	if len(e.AllowedDomains) > 0 || len(e.BlockedDomains) > 0 {
		var level EnforcementLevel
		var detail string
		switch providerName {
		case "aws", "remote", "local":
			if e.Mode == "enforce" {
				level = Enforced
				detail = "Per-agent Envoy proxy with domain-based CONNECT filtering + iptables DROP rules"
			} else {
				level = ValidateOnly
				detail = "Warnings only; set mode: enforce to activate egress proxy"
			}
		default:
			level = NotApplicable
			detail = fmt.Sprintf("Unknown provider %q", providerName)
		}
		reports = append(reports, RuleReport{
			Section: "egress",
			Rule:    "domain_allowlist",
			Level:   level,
			Detail:  detail,
		})
	}

	return reports
}
```

This unifies all three providers under the same mode-driven logic. The provider name no longer changes the egress enforcement level — only the `mode` field does. The detail string now mentions iptables DROP rules since all providers will enforce them.

---

## Phase 5: Tests & Documentation

### 5.1 Update enforcement report tests

**File**: `cli/internal/policy/policy_test.go`

Update `TestEnforcementReportAWS` (line ~251) — with the new default of `enforce`, a policy with no explicit mode should report `Enforced`:
```go
func TestEnforcementReportAWS(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: "enforce"},
		Posture:    &PostureDeclarations{SecretsBackend: "managed", Monitoring: "standard"},
	}
	reports := pf.EnforcementReport("aws")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != Enforced {
			t.Errorf("aws enforce mode: expected enforced, got %s", r.Level)
		}
		// ... posture checks unchanged ...
	}
}
```

Add `TestEnforcementReportAWSValidate`:
```go
func TestEnforcementReportAWSValidate(t *testing.T) {
	pf := &PolicyFile{
		APIVersion: CurrentAPIVersion,
		Egress:     &EgressPolicy{AllowedDomains: []string{"api.anthropic.com"}, Mode: "validate"},
	}
	reports := pf.EnforcementReport("aws")
	for _, r := range reports {
		if r.Rule == "domain_allowlist" && r.Level != ValidateOnly {
			t.Errorf("aws validate mode: expected validate-only, got %s", r.Level)
		}
	}
}
```

Update `TestEnforcementReportRemote` (line ~271) — same pattern, test both modes explicitly.

Update `TestEnforcementReportLocal` (line ~221) — currently tests with `Mode: "validate"` which should still pass. The test at line ~238 (`TestEnforcementReportLocalEnforce`) also still passes.

Add `TestDefaultModeIsEnforce`:
```go
func TestDefaultModeIsEnforce(t *testing.T) {
	pf, err := Load("testdata/no-mode-policy.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if pf.Egress.Mode != "enforce" {
		t.Errorf("expected default mode 'enforce', got %q", pf.Egress.Mode)
	}
}
```

With test fixture `testdata/no-mode-policy.yaml`:
```yaml
apiVersion: conga.dev/v1alpha1
egress:
  allowed_domains:
    - api.anthropic.com
```

### 5.2 Update `conga-policy.yaml.example`

Replace:
```yaml
  # Enforcement mode (local provider only; remote/AWS always enforce).
  #   validate — warn about unenforced rules (default)
  #   enforce  — activate egress proxy container
  mode: validate
```

With:
```yaml
  # Enforcement mode (all providers).
  #   enforce  — activate per-agent egress proxy + iptables DROP rules (default)
  #   validate — warn about unenforced rules only
  mode: enforce
```

### 5.3 Update policy schema spec example

**File**: `specs/2026-03-25_feature_policy-schema/spec.md` (line ~1058)

Same comment update as above.

### 5.4 Update security standards

**File**: `product-knowledge/standards/security.md`

In the Enforcement Escalation table (line ~49), update the Egress filtering row for Enterprise (Prod):

Replace:
```
Per-agent Envoy proxy with domain allowlist. No iptables enforcement (deferred — Phase 3). Blocked attempts logged.
```

With:
```
Per-agent Envoy proxy with domain allowlist + iptables DROP rules. Blocked attempts logged.
```

Update the "Cooperative proxy enforcement" residual risk entry (line ~134) to note that all providers now have iptables enforcement:

Replace:
```
Cooperative proxy enforcement | Low | Egress proxy is set via `HTTPS_PROXY` env var and enforced by iptables DROP rules in the DOCKER-USER chain...
```

Update detail to clarify all providers now enforce.

### 5.5 Update CLAUDE.md

In the "Known Limitations" section, remove or update any references to AWS lacking iptables enforcement.

In the `conga-policy.yaml.example` comment reference in the Secrets section, no change needed.

---

## Edge Cases

| Scenario | Expected Behavior |
|----------|------------------|
| No `mode` field in YAML | Defaults to `enforce` (proxy + iptables active) |
| `mode: validate` with domains | All providers: log warning, no proxy, no iptables |
| `mode: enforce` with domains | All providers: start proxy, apply iptables DROP rules |
| `mode: enforce` with no domains | No proxy (no domains to filter) |
| Agent override with different mode | Agent's mode overrides global (per existing `MergeForAgent()` shallow merge) |
| Policy file doesn't exist | No proxy (nil policy, no-op — unchanged) |
| Transition enforce → validate | `RefreshAgent` / `RefreshAll` removes proxy; systemd `ExecStopPost` cleans iptables |
| Transition validate → enforce | `RefreshAgent` / `RefreshAll` starts proxy; systemd `ExecStartPost` applies iptables |
| Container restart (IP change) | Systemd `ExecStartPost` applies rules with new IP; `ExecStopPost` cleans old rules |
| Docker daemon restart | Systemd restarts containers; `ExecStartPost` re-applies iptables rules |
| Host reboot (AWS) | `conga-image-refresh.service` runs, agent services start, `ExecStartPost` applies iptables |

## Migration Impact

- **Operators with `mode: enforce`**: Zero change. Proxy and iptables active as before (local/remote). AWS gains iptables rules.
- **Operators with `mode: validate`**: Zero change on local (already worked). Remote will now correctly skip the proxy — this is the behavior they asked for.
- **Operators with no mode field**: The default is now `enforce`. On local, this is a **behavioral change** — previously local defaulted to `validate`. On remote/AWS, no change since they were already enforcing. Operators who want warn-only must now explicitly set `mode: validate`.
- **Existing AWS deployments**: Gain iptables DROP rules on next `RefreshAll` or host cycle. This strengthens security — a container that bypasses `HTTPS_PROXY` can no longer reach the internet directly.
