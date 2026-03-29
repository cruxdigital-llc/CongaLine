<!--
GLaDOS-MANAGED DOCUMENT
Last Updated: 2026-03-29
To modify: Edit directly.
-->

# Egress Controls — Deployment Stages & Security Scenarios

> How Conga Line restricts agent outbound network access across the three
> deployment stages. All stages apply iptables DROP rules to force traffic
> through the Envoy proxy — the proxy itself determines whether to block
> or log based on the policy mode.

## Deployment Stages

```mermaid
flowchart TB
    subgraph stage1["Stage 1: No Policy &mdash; Secure by Default"]
        direction TB
        s1_desc["Envoy proxy deployed with empty allowlist
        iptables DROP rules active
        All outbound traffic blocked"]
        s1_desc --> s1_http["HTTP/HTTPS request
        via proxy"]
        s1_desc --> s1_direct["Direct TCP connection
        ignores HTTP_PROXY"]
        s1_http --> s1_deny["Proxy: 403 Denied
        empty allowlist = deny all"]:::deny
        s1_direct --> s1_drop["iptables: DROP
        only subnet traffic allowed"]:::deny
        s1_deny --> s1_result["Agent fully isolated
        No outbound connectivity"]:::result
        s1_drop --> s1_result
    end

    subgraph stage2["Stage 2: Validate Mode &mdash; Build Your Allowlist"]
        direction TB
        s2_desc["Envoy proxy deployed with allowlist
        iptables DROP rules active
        Proxy logs violations, allows traffic through"]
        s2_desc --> s2_allowed["Allowlisted domain
        e.g. api.anthropic.com"]
        s2_desc --> s2_blocked["Non-allowlisted domain
        e.g. example.com"]
        s2_desc --> s2_direct["Direct TCP connection
        ignores HTTP_PROXY"]
        s2_allowed --> s2_ok["Proxy: 200 OK
        domain on allowlist"]:::allow
        s2_blocked --> s2_log["Proxy: 200 OK + logWarn
        egress-validate: would deny example.com"]:::warn
        s2_direct --> s2_drop["iptables: DROP
        forced through proxy"]:::deny
        s2_ok --> s2_result["Full visibility into all traffic
        Admin reviews logs to tune allowlist"]:::result
        s2_log --> s2_result
        s2_drop --> s2_result
    end

    subgraph stage3["Stage 3: Enforce Mode &mdash; Locked Down"]
        direction TB
        s3_desc["Envoy proxy deployed with allowlist
        iptables DROP rules active
        Proxy blocks non-allowlisted traffic"]
        s3_desc --> s3_allowed["Allowlisted domain
        e.g. api.anthropic.com"]
        s3_desc --> s3_blocked["Non-allowlisted domain
        e.g. example.com"]
        s3_desc --> s3_direct["Direct TCP connection
        ignores HTTP_PROXY"]
        s3_allowed --> s3_ok["Proxy: 200 OK
        domain on allowlist"]:::allow
        s3_blocked --> s3_deny["Proxy: 403 Denied
        egress denied: example.com"]:::deny
        s3_direct --> s3_drop["iptables: DROP
        forced through proxy"]:::deny
        s3_ok --> s3_result["Agent restricted to allowlisted domains
        No bypass possible"]:::result
        s3_deny --> s3_result
        s3_drop --> s3_result
    end

    stage1 -->|"conga policy set-egress
    --mode validate
    --allowed-domains ..."| stage2
    stage2 -->|"conga policy set-egress
    --mode enforce"| stage3

    classDef deny fill:#c0392b,stroke:#e74c3c,color:#fff
    classDef allow fill:#0d7377,stroke:#14a3a8,color:#fff
    classDef warn fill:#d4ac0d,stroke:#f1c40f,color:#000
    classDef result fill:#2c3e50,stroke:#34495e,color:#ecf0f1
```

## Key Design Decisions

### iptables applied in ALL modes

iptables DROP rules are always active, regardless of egress policy mode.
Without them, tools that ignore `HTTP_PROXY`/`HTTPS_PROXY` environment
variables (direct TCP connections, `curl --noproxy`, etc.) bypass the
proxy entirely — creating blind spots in validate-mode violation logs
and enforcement gaps in enforce mode.

The proxy's Lua filter handles the mode distinction:

| Mode | Proxy Behavior | iptables Behavior |
|------|---------------|-------------------|
| **No policy** | Deny all (empty allowlist) | DROP all non-subnet traffic |
| **Validate** | Log violations, allow through | DROP all non-subnet traffic |
| **Enforce** | Block non-allowlisted (403) | DROP all non-subnet traffic |

### Defense in depth layers

```mermaid
flowchart LR
    agent["Agent Container"] --> proxy["Envoy Proxy
    Domain-level filtering
    CONNECT method inspection"]
    agent -.->|"Direct connection
    attempt"| iptables["iptables DOCKER-USER
    DROP all non-subnet"]
    proxy --> internet["Internet"]
    iptables -.->|"Blocked"| nowhere["Dropped"]

    classDef block fill:#c0392b,stroke:#e74c3c,color:#fff
    class iptables,nowhere block
```

Two independent enforcement layers ensure no single failure compromises
egress controls:

1. **Envoy proxy** — Application-layer filtering. Inspects CONNECT
   requests, matches domain against allowlist, applies mode-specific
   behavior (log or block).

2. **iptables DROP rules** — Network-layer enforcement. Ensures all
   outbound traffic from the agent container can only reach the local
   Docker subnet (where the proxy lives). Any direct internet-bound
   connection is dropped.

### Per-agent isolation

Each agent gets its own:
- Docker bridge network
- Envoy proxy container
- iptables rule set (keyed by container IP)

No shared proxy means one agent's compromise cannot observe or interfere
with another agent's traffic.

## Operator Workflow

```mermaid
flowchart LR
    A["Deploy agent
    conga admin add-user"] --> B["Agent starts
    deny-all egress"]
    B --> C["Set validate policy
    conga policy set-egress
    --mode validate"]
    C --> D["Deploy policy
    conga policy deploy"]
    D --> E["Run agent workload
    observe proxy logs"]
    E --> F["Review violations
    conga get-proxy-logs"]
    F -->|"Tune allowlist"| C
    F -->|"Allowlist complete"| G["Switch to enforce
    conga policy set-egress
    --mode enforce"]
    G --> H["Deploy enforce
    conga policy deploy"]
    H --> I["Agent locked down
    only allowlisted domains"]
```
