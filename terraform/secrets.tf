locals {
  shared_secrets = {
    "openclaw/shared/slack-bot-token" = "Slack bot token (xoxb-)"
    "openclaw/shared/slack-app-token" = "Slack app token (xapp-)"
  }

  myagent_secrets = {
    "openclaw/myagent/anthropic-api-key" = "Anthropic API key"
    "openclaw/myagent/trello-api-key"    = "Trello API key"
    "openclaw/myagent/trello-token"      = "Trello token"
  }

  all_secrets = merge(local.shared_secrets, local.myagent_secrets)
}

resource "aws_secretsmanager_secret" "openclaw" {
  for_each    = local.all_secrets
  name        = each.key
  description = each.value

  tags = {
    Name = each.key
  }
}

resource "aws_secretsmanager_secret_version" "openclaw" {
  for_each      = local.all_secrets
  secret_id     = aws_secretsmanager_secret.openclaw[each.key].id
  secret_string = "REPLACE_ME"

  lifecycle {
    ignore_changes = [secret_string]
  }
}
