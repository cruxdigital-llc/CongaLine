// Package iptables provides shared iptables command generation for egress enforcement.
// Providers call these pure functions to get shell command strings, then execute
// them via their own mechanism (local exec, SSH, or Docker nsenter).
package iptables

import (
	"fmt"
	"net"
)

// AddRulesCmd returns a shell command that idempotently inserts egress DROP rules
// into DOCKER-USER for the given container IP. Rules are inserted in reverse order
// (iptables -I pushes to top) so the final chain order is:
//
//  1. ESTABLISHED,RELATED → RETURN (allow response traffic)
//  2. dst=subnet → RETURN (allow proxy + Docker DNS)
//  3. DROP (block everything else from this source)
//
// Returns an error if containerIP or subnetCIDR are not well-formed.
func AddRulesCmd(containerIP, subnetCIDR string) (string, error) {
	if err := validateIP(containerIP); err != nil {
		return "", fmt.Errorf("invalid container IP: %w", err)
	}
	if err := validateCIDR(subnetCIDR); err != nil {
		return "", fmt.Errorf("invalid subnet CIDR: %w", err)
	}
	return fmt.Sprintf(
		"iptables -C DOCKER-USER -s %s -j DROP 2>/dev/null || iptables -I DOCKER-USER -s %s -j DROP; "+
			"iptables -C DOCKER-USER -s %s -d %s -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s %s -d %s -j RETURN; "+
			"iptables -C DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || iptables -I DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN",
		containerIP, containerIP,
		containerIP, subnetCIDR, containerIP, subnetCIDR,
		containerIP, containerIP), nil
}

// RemoveRulesCmd returns a shell command that removes egress iptables rules
// for the given container IP. Idempotent — each deletion is wrapped with || true.
// Returns empty string if containerIP is empty.
func RemoveRulesCmd(containerIP, subnetCIDR string) string {
	if containerIP == "" {
		return ""
	}
	return fmt.Sprintf(
		"iptables -D DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null || true; "+
			"iptables -D DOCKER-USER -s %s -d %s -j RETURN 2>/dev/null || true; "+
			"iptables -D DOCKER-USER -s %s -j DROP 2>/dev/null || true",
		containerIP,
		containerIP, subnetCIDR,
		containerIP)
}

// CheckRulesCmd returns a shell command that checks whether all three egress rules
// exist for the given container IP. Exits 0 only if all rules are present.
func CheckRulesCmd(containerIP, subnetCIDR string) string {
	return fmt.Sprintf(
		"iptables -C DOCKER-USER -s %s -m conntrack --ctstate ESTABLISHED,RELATED -j RETURN 2>/dev/null && "+
			"iptables -C DOCKER-USER -s %s -d %s -j RETURN 2>/dev/null && "+
			"iptables -C DOCKER-USER -s %s -j DROP 2>/dev/null",
		containerIP,
		containerIP, subnetCIDR,
		containerIP)
}

func validateIP(ip string) error {
	if net.ParseIP(ip) == nil {
		return fmt.Errorf("%q is not a valid IP address", ip)
	}
	return nil
}

func validateCIDR(cidr string) error {
	_, _, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%q is not a valid CIDR: %w", cidr, err)
	}
	return nil
}
