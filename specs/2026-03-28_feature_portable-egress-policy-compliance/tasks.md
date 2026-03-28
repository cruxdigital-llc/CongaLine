# Implementation Tasks — Portable Egress Policy Compliance

## Phase 1: Default Mode Change
- [x] 1.1 Normalize empty egress mode to `enforce` in `policy.go` Load/MergeForAgent
- [x] 1.2 Run existing policy tests to verify no regressions

## Phase 2: Remote Provider — Respect `mode` field
- [x] 2.1 Update `ProvisionAgent` egress block to check `egressPolicy.Mode`
- [x] 2.2 Update `RefreshAgent` egress block to check `egressPolicy.Mode`
- [x] 2.3 Update `ensureEgressIptables` to check mode and fix comment
- [x] 2.4 Run `go build` to verify compilation

## Phase 3: AWS Bootstrap — Respect `mode` + iptables
- [x] 3.1 Add mode parsing to `generate_egress_conf()` in `user-data.sh.tftpl`
- [x] 3.2 Add mode check to skip config generation when validate
- [x] 3.3 Add iptables DROP rules after proxy startup in bootstrap
- [x] 3.4 Add iptables to systemd unit (ExecStartPost / ExecStopPost)
- [x] 3.5 Add iptables to `refresh-user.sh.tmpl`
- [x] 3.6 Add iptables cleanup to bootstrap agent removal section

## Phase 4: Enforcement Report
- [x] 4.1 Update `egressReport()` in `enforcement.go` to be mode-driven for all providers

## Phase 5: Tests & Documentation
- [x] 5.1 Update enforcement report tests (AWS, remote, local)
- [x] 5.2 Add default mode normalization test
- [x] 5.3 Update `conga-policy.yaml.example` comments and default
- [x] 5.4 Update `security.md` enforcement escalation table
- [x] 5.5 Run full test suite — all packages pass
