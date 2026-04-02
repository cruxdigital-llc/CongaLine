output "agent_ports" {
  description = "Map of agent name to gateway port"
  value       = { for k, v in conga_agent.this : k => v.gateway_port }
}

output "agent_ids" {
  description = "Map of agent name to resource ID"
  value       = { for k, v in conga_agent.this : k => v.id }
}
