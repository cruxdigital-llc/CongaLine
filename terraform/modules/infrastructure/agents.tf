# Upload agent overlay + default files to S3 for deployment to agent workspaces.
# Files under _defaults/ provide shared defaults; files under <name>/ override
# the defaults for specific agents. Deployed during bootstrap and on every
# container restart via deploy-agents.sh.
#
# The S3 prefix uses "conga/agents/" (post-2026-05-XX rename) instead of the
# legacy "conga/behavior/" — the rename PR included a one-shot terraform
# state migration via the `moved {}` blocks below so existing state doesn't
# trigger a destroy + recreate.

locals {
  agent_files = {
    for f in fileset("${var.repo_root}/agents", "**/*") : f => f
    if !endswith(f, ".gitkeep")
  }
}

resource "aws_s3_object" "agents" {
  for_each = local.agent_files
  bucket   = local.state_bucket
  key      = "conga/agents/${each.value}"
  content  = file("${var.repo_root}/agents/${each.value}")
  etag     = md5(file("${var.repo_root}/agents/${each.value}"))
}

moved {
  from = aws_s3_object.behavior
  to   = aws_s3_object.agents
}

# Upload deploy helper to S3 — single source of truth for bootstrap and provisioning
resource "aws_s3_object" "deploy_agents_helper" {
  bucket  = local.state_bucket
  key     = "conga/scripts/deploy-agents.sh"
  content = file("${var.repo_root}/scripts/deploy-agents.sh.tmpl")
  etag    = md5(file("${var.repo_root}/scripts/deploy-agents.sh.tmpl"))
}

moved {
  from = aws_s3_object.deploy_behavior_helper
  to   = aws_s3_object.deploy_agents_helper
}

# Upload pre-start helper — called by ExecStartPre in agent systemd units
resource "aws_s3_object" "pre_start_helper" {
  bucket  = local.state_bucket
  key     = "conga/scripts/pre-start.sh"
  content = file("${var.repo_root}/scripts/pre-start.sh.tmpl")
  etag    = md5(file("${var.repo_root}/scripts/pre-start.sh.tmpl"))
}

# Restart all agents when overlay files or the deploy helper change.
# NOTE: This restarts every agent, not just the ones whose files changed.
# Acceptable for small fleets (2-5 agents); for larger deployments,
# consider scoping restarts to affected agents only.
# The ExecStartPre in each agent's systemd unit syncs from S3 and runs
# deploy-agents.sh, so a restart is sufficient to pick up changes.
locals {
  agent_content_hash = md5(join("", [
    for f in sort(keys(local.agent_files)) :
    md5(file("${var.repo_root}/agents/${f}"))
  ]))
  deploy_helper_hash    = md5(file("${var.repo_root}/scripts/deploy-agents.sh.tmpl"))
  pre_start_helper_hash = md5(file("${var.repo_root}/scripts/pre-start.sh.tmpl"))
}

resource "terraform_data" "agents_refresh" {
  depends_on = [
    aws_s3_object.agents,
    aws_s3_object.deploy_agents_helper,
    aws_s3_object.pre_start_helper,
    terraform_data.bootstrap_ready,
  ]

  triggers_replace = "${local.agent_content_hash}-${local.deploy_helper_hash}-${local.pre_start_helper_hash}"

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      echo "Agent overlay files changed — restarting all agents..."
      INSTANCE_ID="${aws_instance.conga.id}"
      REGION="${var.aws_region}"
      PROFILE="${var.aws_profile}"
      TMPJSON=$(mktemp)
      printf '{"InstanceIds":["%s"],"DocumentName":"AWS-RunShellScript","TimeoutSeconds":120,"Parameters":{"commands":["systemctl list-units --type=service --state=running --no-legend conga-*.service | grep -v router | cut -d\\\\  -f1 | while read svc; do echo Restarting $svc; systemctl restart $svc; sleep 2; done; echo DONE"]}}' "$INSTANCE_ID" > "$TMPJSON"
      RESULT=$(aws ssm send-command \
        --cli-input-json "file://$TMPJSON" \
        --region "$REGION" \
        --profile "$PROFILE" \
        --output text --query "Command.CommandId" 2>/dev/null)
      rm -f "$TMPJSON"
      if [ -z "$RESULT" ]; then
        echo "WARNING: Failed to send restart command — agents will pick up changes on next manual restart"
        exit 0
      fi
      OUTPUT=""
      for i in $(seq 1 12); do
        sleep 10
        OUTPUT=$(aws ssm get-command-invocation \
          --command-id "$RESULT" \
          --instance-id "$INSTANCE_ID" \
          --region "$REGION" \
          --profile "$PROFILE" \
          --output text --query "[Status, StandardOutputContent]" 2>/dev/null || echo "")
        echo "Poll $i/12: $OUTPUT"
        if echo "$OUTPUT" | grep -qE "(Success|DONE)"; then
          echo "All agents restarted with updated overlay files."
          break
        fi
        if echo "$OUTPUT" | grep -qE "(Failed|TimedOut|Cancelled)"; then
          echo "WARNING: Agent restart command failed — check 'conga status' manually"
          break
        fi
      done
      if ! echo "$OUTPUT" | grep -qE "(Success|DONE|Failed|TimedOut|Cancelled)"; then
        echo "WARNING: Agent restart may not have completed after 120s — check 'conga status' manually"
      fi
    EOT
  }
}

moved {
  from = terraform_data.behavior_refresh
  to   = terraform_data.agents_refresh
}
