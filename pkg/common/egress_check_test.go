package common

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/runtime"
)

func TestCheckOverlayEgress_NilOverlay(t *testing.T) {
	if got := CheckOverlayEgress(nil, []string{"anything"}); got != nil {
		t.Fatalf("nil overlay should return nil, got %v", got)
	}
}

func TestCheckOverlayEgress_NoBaseURLs(t *testing.T) {
	// Hosted Anthropic primary (no overlay.Model) + no subagent → no hosts to check.
	o := &runtime.AgentOverlay{Version: 2}
	if got := CheckOverlayEgress(o, nil); got != nil {
		t.Fatalf("overlay with no hosts should return nil, got %v", got)
	}
}

func TestCheckOverlayEgress_HostedOpenAIPrimary_NoBaseURL(t *testing.T) {
	// Empty base_url (hosted OpenAI) doesn't need an allowlist entry.
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "gpt-5.5",
			BaseURL:  "",
		},
	}
	if got := CheckOverlayEgress(o, nil); got != nil {
		t.Fatalf("hosted openai primary should not require allowlist entry, got %v", got)
	}
}

func TestCheckOverlayEgress_PrimaryMissing(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://192.168.181.97:11434",
		},
	}
	got := CheckOverlayEgress(o, []string{"api.anthropic.com"})
	want := []string{"192.168.181.97"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestCheckOverlayEgress_SubagentMissing(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 2,
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}
	got := CheckOverlayEgress(o, []string{"api.anthropic.com"})
	want := []string{"litellm.lan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestCheckOverlayEgress_BothPresent(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://spark.lan:11434",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}
	allowlist := []string{"spark.lan", "litellm.lan", "api.anthropic.com"}
	if got := CheckOverlayEgress(o, allowlist); got != nil {
		t.Fatalf("all endpoints present should return nil, got %v", got)
	}
}

func TestCheckOverlayEgress_OnlySubagentMissing(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen3:6b",
			BaseURL:  "http://spark.lan:11434",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-2.5-72b-instruct",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}
	got := CheckOverlayEgress(o, []string{"spark.lan", "api.anthropic.com"})
	want := []string{"litellm.lan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func TestCheckOverlayEgress_CaseInsensitive(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "x",
			BaseURL:  "https://LiteLLM.LAN/v1",
		},
	}
	if got := CheckOverlayEgress(o, []string{"litellm.lan"}); got != nil {
		t.Fatalf("case-insensitive match expected, got %v", got)
	}
	if got := CheckOverlayEgress(o, []string{"LITELLM.LAN"}); got != nil {
		t.Fatalf("uppercase allowlist entry should match, got %v", got)
	}
}

func TestCheckOverlayEgress_WildcardMatch(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "x",
			BaseURL:  "https://api.openai.com/v1",
		},
	}
	if got := CheckOverlayEgress(o, []string{"*.openai.com"}); got != nil {
		t.Fatalf("*.openai.com should match api.openai.com, got %v", got)
	}
}

func TestCheckOverlayEgress_WildcardDoesNotMatchBare(t *testing.T) {
	// Matches policy.MatchDomain semantics: *.openai.com matches subdomains
	// but NOT the bare openai.com.
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "x",
			BaseURL:  "https://openai.com/v1",
		},
	}
	got := CheckOverlayEgress(o, []string{"*.openai.com"})
	want := []string{"openai.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want %v (wildcard should not match bare), got %v", want, got)
	}
}

func TestCheckOverlayEgress_PortStripped(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen",
			BaseURL:  "http://spark.lan:11434",
		},
	}
	if got := CheckOverlayEgress(o, []string{"spark.lan"}); got != nil {
		t.Fatalf("port should not affect matching, got %v", got)
	}
}

func TestCheckOverlayEgress_DuplicateHostDedup(t *testing.T) {
	// Primary and subagent on the same host — gap should appear once.
	o := &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "primary",
			BaseURL:  "https://litellm.lan/v1",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "subagent",
				BaseURL:  "https://litellm.lan/v1",
			},
		},
	}
	got := CheckOverlayEgress(o, nil)
	want := []string{"litellm.lan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate hosts should dedup; want %v, got %v", want, got)
	}
}

func TestCheckOverlayEgress_OrderPrimaryThenSubagent(t *testing.T) {
	// Insertion order: primary host first, then subagent host (when both missing).
	o := &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen",
			BaseURL:  "http://primary.lan:11434",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-72b",
				BaseURL:  "https://subagent.lan/v1",
			},
		},
	}
	got := CheckOverlayEgress(o, nil)
	want := []string{"primary.lan", "subagent.lan"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected primary-first order; want %v, got %v", want, got)
	}
}

func TestCheckOverlayEgress_MalformedBaseURLSkipped(t *testing.T) {
	// Validation rejects malformed URLs before the overlay reaches this
	// helper, but defense-in-depth: don't crash on a bad URL passed
	// programmatically. Treat unparseable hosts as if absent.
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOpenAI,
			Name:     "x",
			BaseURL:  "http://[::1", // malformed (matches the URL-shape test in overlay_test.go)
		},
	}
	if got := CheckOverlayEgress(o, nil); got != nil {
		t.Fatalf("malformed URL should be skipped, got %v", got)
	}
}

func TestFormatEgressGapWarning_MultilineContainsAgentAndHost(t *testing.T) {
	got := FormatEgressGapWarning("code-dev", "litellm.lan")
	if !strings.Contains(got, "code-dev") {
		t.Fatalf("warning should mention agent name, got %q", got)
	}
	if !strings.Contains(got, "litellm.lan") {
		t.Fatalf("warning should mention host, got %q", got)
	}
	if !strings.Contains(got, "egress_allowed_domains") {
		t.Fatalf("warning should mention tfvars field, got %q", got)
	}
	if !strings.Contains(got, "conga-policy.yaml") {
		t.Fatalf("warning should mention policy file, got %q", got)
	}
}

func TestWarnOverlayEgressGaps_NoGaps_NoOutput(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 1,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen",
			BaseURL:  "http://spark.lan:11434",
		},
	}
	var buf bytes.Buffer
	WarnOverlayEgressGaps(&buf, o, []string{"spark.lan"}, "test")
	if buf.Len() != 0 {
		t.Fatalf("no gaps → no output expected, got %q", buf.String())
	}
}

func TestWarnOverlayEgressGaps_OneWarningPerGap(t *testing.T) {
	o := &runtime.AgentOverlay{
		Version: 2,
		Model: &runtime.ModelOverlay{
			Provider: runtime.ProviderOllama,
			Name:     "qwen",
			BaseURL:  "http://primary.lan:11434",
		},
		Subagents: &runtime.SubagentsOverlay{
			Model: &runtime.ModelOverlay{
				Provider: runtime.ProviderOpenAI,
				Name:     "qwen-72b",
				BaseURL:  "https://subagent.lan/v1",
			},
		},
	}
	var buf bytes.Buffer
	WarnOverlayEgressGaps(&buf, o, nil, "test")
	got := buf.String()
	if strings.Count(got, "warning:") != 2 {
		t.Fatalf("expected 2 warnings (primary + subagent), got %d: %q", strings.Count(got, "warning:"), got)
	}
	if !strings.Contains(got, "primary.lan") || !strings.Contains(got, "subagent.lan") {
		t.Fatalf("warnings should name both hosts, got %q", got)
	}
}
