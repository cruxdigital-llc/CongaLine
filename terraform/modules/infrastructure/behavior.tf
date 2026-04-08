# Upload behavior files to S3 for deployment to agent workspaces.
# Files under default/ provide shared defaults; files under agents/<name>/
# override the defaults for specific agents. Deployed during bootstrap
# and on every container restart via deploy-behavior.sh.

locals {
  behavior_files = {
    for f in fileset("${var.repo_root}/behavior", "**/*") : f => f
    if !endswith(f, ".gitkeep")
  }
}

resource "aws_s3_object" "behavior" {
  for_each = local.behavior_files
  bucket   = local.state_bucket
  key      = "conga/behavior/${each.value}"
  content  = file("${var.repo_root}/behavior/${each.value}")
  etag     = md5(file("${var.repo_root}/behavior/${each.value}"))
}

# Upload deploy helper to S3 — single source of truth for bootstrap and provisioning
resource "aws_s3_object" "deploy_behavior_helper" {
  bucket  = local.state_bucket
  key     = "conga/scripts/deploy-behavior.sh"
  content = file("${var.repo_root}/scripts/deploy-behavior.sh.tmpl")
  etag    = md5(file("${var.repo_root}/scripts/deploy-behavior.sh.tmpl"))
}

# Restart all agents when behavior files or the deploy helper change.
# NOTE: This restarts every agent, not just the ones whose files changed.
# Acceptable for small fleets (2-5 agents); for larger deployments,
# consider scoping restarts to affected agents only.
# The ExecStartPre in each agent's systemd unit syncs from S3 and runs
# deploy-behavior.sh, so a restart is sufficient to pick up changes.
locals {
  behavior_content_hash = md5(join("", [
    for f in sort(keys(local.behavior_files)) :
    md5(file("${var.repo_root}/behavior/${f}"))
  ]))
  deploy_helper_hash = md5(file("${var.repo_root}/scripts/deploy-behavior.sh.tmpl"))
}

resource "terraform_data" "behavior_refresh" {
  depends_on = [
    aws_s3_object.behavior,
    aws_s3_object.deploy_behavior_helper,
    terraform_data.bootstrap_ready,
  ]

  triggers_replace = "${local.behavior_content_hash}-${local.deploy_helper_hash}"

  provisioner "local-exec" {
    interpreter = ["bash", "-c"]
    command     = <<-EOT
      echo "Behavior files changed — restarting all agents..."
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
      sleep 30
      OUTPUT=$(aws ssm get-command-invocation \
        --command-id "$RESULT" \
        --instance-id "$INSTANCE_ID" \
        --region "$REGION" \
        --profile "$PROFILE" \
        --output text --query "[Status, StandardOutputContent]" 2>/dev/null)
      echo "$OUTPUT"
      if echo "$OUTPUT" | grep -q "DONE"; then
        echo "All agents restarted with updated behavior files."
      else
        echo "WARNING: Agent restart may not have completed — check 'conga status' manually"
      fi
    EOT
  }
}
