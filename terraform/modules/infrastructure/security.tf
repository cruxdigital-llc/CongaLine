# --- Locals: resolve "vpc" CIDR shorthand ---

locals {
  egress_ports = {
    for i, p in var.egress_ports : "${p.protocol}-${p.port}" => merge(p, {
      index         = i
      resolved_cidr = p.cidr == "vpc" ? aws_vpc.main.cidr_block : p.cidr
    })
  }

  has_tcp_egress = length([for p in var.egress_ports : p if p.protocol == "tcp"]) > 0
  has_udp_egress = length([for p in var.egress_ports : p if p.protocol == "udp"]) > 0

  has_vpc_udp = length([for p in var.egress_ports : p if p.protocol == "udp" && p.cidr == "vpc"]) > 0

  wan_udp_ports = {
    for i, p in var.egress_ports : "${p.protocol}-${p.port}" => merge(p, {
      index         = i
      resolved_cidr = p.cidr == "vpc" ? aws_vpc.main.cidr_block : p.cidr
    }) if p.protocol == "udp" && p.cidr != "vpc"
  }
}

# --- Security Group: Conga Line Host (zero ingress) ---

resource "aws_security_group" "conga_host" {
  name_prefix = "${var.project_name}-host-"
  description = "Conga Line host - zero ingress, configurable egress"
  vpc_id      = aws_vpc.main.id

  tags = {
    Name = "${var.project_name}-host-sg"
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_egress_rule" "egress" {
  for_each          = local.egress_ports
  security_group_id = aws_security_group.conga_host.id
  description       = each.value.description != "" ? each.value.description : "${each.value.protocol}/${each.value.port}"
  ip_protocol       = each.value.protocol
  from_port         = each.value.port
  to_port           = each.value.port
  cidr_ipv4         = each.value.resolved_cidr
}

# --- NACLs: Private Subnet (defense-in-depth) ---

resource "aws_network_acl" "private" {
  vpc_id     = aws_vpc.main.id
  subnet_ids = [aws_subnet.private.id]

  tags = {
    Name = "${var.project_name}-private-nacl"
  }
}

# Outbound: one rule per egress port
resource "aws_network_acl_rule" "private_egress" {
  for_each       = local.egress_ports
  network_acl_id = aws_network_acl.private.id
  rule_number    = 100 + each.value.index
  egress         = true
  protocol       = each.value.protocol
  rule_action    = "allow"
  cidr_block     = each.value.resolved_cidr
  from_port      = each.value.port
  to_port        = each.value.port
}

# Inbound: TCP ephemeral return traffic
resource "aws_network_acl_rule" "private_ingress_tcp_ephemeral" {
  count          = local.has_tcp_egress ? 1 : 0
  network_acl_id = aws_network_acl.private.id
  rule_number    = 100
  egress         = false
  protocol       = "tcp"
  rule_action    = "allow"
  cidr_block     = "0.0.0.0/0"
  from_port      = 1024
  to_port        = 65535
}

# Inbound: UDP responses from VPC (DNS responses on port 53)
resource "aws_network_acl_rule" "private_ingress_udp_vpc" {
  count          = local.has_vpc_udp ? 1 : 0
  network_acl_id = aws_network_acl.private.id
  rule_number    = 110
  egress         = false
  protocol       = "udp"
  rule_action    = "allow"
  cidr_block     = aws_vpc.main.cidr_block
  from_port      = 53
  to_port        = 53
}

# Inbound: UDP return traffic from WAN, scoped to each egress port's CIDR.
# When a static IP is available, set cidr on the egress port entry in tfvars
# to lock down both outbound and return traffic.
resource "aws_network_acl_rule" "private_ingress_udp_wan" {
  for_each       = local.wan_udp_ports
  network_acl_id = aws_network_acl.private.id
  rule_number    = 120 + each.value.index
  egress         = false
  protocol       = "udp"
  rule_action    = "allow"
  cidr_block     = each.value.resolved_cidr
  from_port      = 1024
  to_port        = 65535
}
