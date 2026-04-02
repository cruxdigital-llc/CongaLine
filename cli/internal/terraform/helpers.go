package terraform

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"

	congaprovider "github.com/cruxdigital-llc/conga-line/cli/internal/provider"
)

// agentNameRegex matches valid agent names: lowercase alphanumeric with hyphens.
var agentNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// secretNameRegex matches valid secret names: kebab-case.
var secretNameRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// isNotFoundErr returns true if the error indicates a resource was not found.
// Used by Read methods to distinguish "deleted externally" from transient failures.
func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such") ||
		strings.Contains(msg, "does not exist")
}

// splitImportID splits an import ID by "/" into exactly n parts.
// Returns nil if the format doesn't match.
func splitImportID(id string, n int) []string {
	parts := strings.SplitN(id, "/", n)
	if len(parts) != n {
		return nil
	}
	for _, p := range parts {
		if p == "" {
			return nil
		}
	}
	return parts
}

// extractProvider extracts the congaprovider.Provider from resource configure data.
func extractProvider(req resource.ConfigureRequest, resp *resource.ConfigureResponse) congaprovider.Provider {
	if req.ProviderData == nil {
		return nil
	}
	p, ok := req.ProviderData.(*congaProvider)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *congaProvider, got %T", req.ProviderData),
		)
		return nil
	}
	return p.prov
}
