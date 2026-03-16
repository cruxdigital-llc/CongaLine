# Feature: Config Integrity + Monitoring — Trace Log

**Started**: 2026-03-16
**Status**: ✅ Verified and closed

## Active Personas
- Architect — monitoring design, alerting, integrity checks

## Decisions
- **CloudWatch agent** over `awslogs` driver — compatible with future Docker rootless (awslogs needs daemon IAM access, which rootless Docker can't get via IMDS)
- **Journal-based log shipping** — CloudWatch agent reads systemd journal, picks up both container logs and config check output
- **5-minute check interval** — configurable via `config_check_interval_minutes` Terraform variable
- **SNS topic with no subscribers** — `alert_email` variable defaults to empty
- **Metric filter approach** — no custom CloudWatch agent metrics needed; log filter + alarm

## Files Created
- [requirements.md](requirements.md)
- [plan.md](plan.md)
- [spec.md](spec.md) — full Terraform + user-data additions

## Persona Review
**Architect**: ✅ Approved. Journal filtering, hash baseline, timer persistence, IAM scope all correct.

## Standards Gate Report
| Standard | Scope | Severity | Verdict |
|---|---|---|---|
| Detect what you can't prevent | monitoring | must | ✅ PASSES |
| Config integrity | config | must | ✅ PASSES |
| Least privilege | iam | must | ✅ PASSES |
| Defense in depth | architecture | must | ✅ PASSES |
