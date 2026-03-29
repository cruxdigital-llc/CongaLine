# Implementation Tasks — Portable Egress Policy Compliance (Pass 2)

## Phase 2: All Providers — Split proxy deployment from iptables enforcement
- [ ] 2.1 Local provider: split `egressEnforce` into `egressProxy` + `egressEnforce` in ProvisionAgent
- [ ] 2.2 Local provider: split in RefreshAgent
- [ ] 2.3 Remote provider: split in ProvisionAgent
- [ ] 2.4 Remote provider: split in RefreshAgent
- [ ] 2.5 Run `go build` to verify compilation

## Phase 3: AWS Bootstrap — Always deploy proxy, iptables in enforce only
- [ ] 3.1 Update `generate_egress_conf()` to always generate config (validate = keep domains for logging)
- [ ] 3.2 Add validate/enforce mode to Lua deny action in bootstrap
- [ ] 3.3 Gate iptables on enforce mode in bootstrap

## Phase 3b: Validate-Mode Lua Filter (Log-but-Allow)
- [ ] 3b.1 Add `mode` parameter to `GenerateProxyConf()` in egress.go
- [ ] 3b.2 Add `ValidateMode` to envoy config template — logWarn instead of 403
- [ ] 3b.3 Update all callers of `GenerateProxyConf()`

## Phase 4: Enforcement Report — Update detail strings
- [ ] 4.1 Update validate-mode detail string to reflect passthrough proxy with logging

## Phase 5: Tests & Documentation
- [ ] 5.1 Update proxy config tests for validate mode output
- [ ] 5.2 Update `conga-policy.yaml.example` comments
- [ ] 5.3 Update `security.md` to reflect validate = proxy with logging
- [ ] 5.4 Run full test suite
