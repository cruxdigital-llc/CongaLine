#!/usr/bin/env bash
# Migrates a host's /opt/conga/behavior/ tree to /opt/conga/agents/ following
# the 2026-05-XX rename. Idempotent: re-runs on partially-migrated hosts
# complete the move; re-runs on fully-migrated hosts are no-ops.
#
# Path overrides (for testing or non-standard installs):
#   OLD_BASE=<path>   override the source root (default: /opt/conga/behavior)
#   NEW_BASE=<path>   override the destination root (default: /opt/conga/agents)
#
# Behavior:
#   - Uses mv (preserves inode, ownership, perms — important for the
#     uid 1000 container user that reads these files at runtime).
#   - Skips entries already at the destination.
#   - Never overwrites destination content; safe if both old and new exist
#     side-by-side.
#   - Removes the now-empty old root only if everything migrated cleanly.

set -euo pipefail

OLD="${OLD_BASE:-/opt/conga/behavior}"
NEW="${NEW_BASE:-/opt/conga/agents}"

if [[ ! -d "$OLD" ]]; then
    echo "[migrate] nothing to do — $OLD does not exist."
    exit 0
fi

mkdir -p "$NEW"

# Move per-agent dirs: $OLD/agents/<name>/  ->  $NEW/<name>/
if [[ -d "$OLD/agents" ]]; then
    for entry in "$OLD/agents"/*; do
        [[ -e "$entry" ]] || continue   # empty dir guard
        name=$(basename "$entry")
        if [[ -d "$NEW/$name" || -f "$NEW/$name.json" && -d "$entry" ]]; then
            # Destination already has either the overlay dir or a colliding
            # name. Skip — operator can resolve manually if intentional.
            if [[ -d "$NEW/$name" ]]; then
                echo "[migrate] skip overlay '$name' — already at $NEW/$name"
            fi
            continue
        fi
        mv "$entry" "$NEW/$name"
        echo "[migrate] moved overlay: agents/$name -> $name"
    done
    # Remove the now-empty $OLD/agents if everything was migrated.
    rmdir "$OLD/agents" 2>/dev/null || true
fi

# Move defaults: $OLD/default/<runtime>/<type>/  ->  $NEW/_defaults/<runtime>/<type>/
if [[ -d "$OLD/default" ]]; then
    if [[ -d "$NEW/_defaults" ]]; then
        echo "[migrate] skip defaults — already at $NEW/_defaults"
    else
        mv "$OLD/default" "$NEW/_defaults"
        echo "[migrate] moved defaults: default/ -> _defaults/"
    fi
fi

# Remove the old root if empty.
if [[ -d "$OLD" ]]; then
    if rmdir "$OLD" 2>/dev/null; then
        echo "[migrate] removed empty $OLD"
    else
        remaining=$(ls -A "$OLD" 2>/dev/null || true)
        if [[ -n "$remaining" ]]; then
            echo "[migrate] warning: $OLD not empty after migration; review manually:"
            ls -la "$OLD"
        fi
    fi
fi

echo "[migrate] complete."
