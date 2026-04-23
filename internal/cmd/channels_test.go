package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cruxdigital-llc/conga-line/pkg/channels"
	"github.com/cruxdigital-llc/conga-line/pkg/provider"
)

func bindings(n int) []channels.ChannelBinding {
	out := make([]channels.ChannelBinding, n)
	for i := range n {
		out[i] = channels.ChannelBinding{Platform: "slack", ID: idAt(i)}
	}
	return out
}

func idAt(i int) string {
	// Just produces distinguishable IDs: C0, C1, C2, ...
	return "C" + string(rune('0'+i))
}

func TestPickBindingFrom_SelectFirst(t *testing.T) {
	in := strings.NewReader("1\n")
	var out bytes.Buffer
	ids, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled {
		t.Fatal("want not cancelled")
	}
	if len(ids) != 1 || ids[0] != "C0" {
		t.Errorf("want [C0], got %v", ids)
	}
}

func TestPickBindingFrom_SelectMiddle(t *testing.T) {
	in := strings.NewReader("2\n")
	var out bytes.Buffer
	ids, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled || len(ids) != 1 || ids[0] != "C1" {
		t.Errorf("want [C1] not cancelled, got %v cancelled=%v", ids, cancelled)
	}
}

func TestPickBindingFrom_All(t *testing.T) {
	in := strings.NewReader("a\n")
	var out bytes.Buffer
	ids, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled {
		t.Fatal("want not cancelled for 'a'")
	}
	if len(ids) != 3 || ids[0] != "C0" || ids[1] != "C1" || ids[2] != "C2" {
		t.Errorf("want [C0 C1 C2], got %v", ids)
	}
}

func TestPickBindingFrom_AllCaseInsensitive(t *testing.T) {
	in := strings.NewReader("ALL\n")
	var out bytes.Buffer
	ids, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cancelled || len(ids) != 2 {
		t.Errorf("want [C0 C1], got %v", ids)
	}
}

func TestPickBindingFrom_EmptyInputCancels(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer
	ids, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("empty input should cancel")
	}
	if ids != nil {
		t.Errorf("want nil ids on cancel, got %v", ids)
	}
}

func TestPickBindingFrom_ExplicitNoCancels(t *testing.T) {
	in := strings.NewReader("n\n")
	var out bytes.Buffer
	_, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("'n' should cancel")
	}
}

func TestPickBindingFrom_EOFCancels(t *testing.T) {
	// No newline, immediate EOF — safe default is cancel.
	in := strings.NewReader("")
	var out bytes.Buffer
	_, cancelled, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Error("EOF should cancel")
	}
}

func TestPickBindingFrom_OutOfRangeErrors(t *testing.T) {
	in := strings.NewReader("99\n")
	var out bytes.Buffer
	_, _, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err == nil {
		t.Fatal("out-of-range input should error")
	}
	if !strings.Contains(err.Error(), "99") {
		t.Errorf("error should mention the bad input, got: %v", err)
	}
}

func TestPickBindingFrom_InvalidInputErrors(t *testing.T) {
	in := strings.NewReader("nope\n")
	var out bytes.Buffer
	_, _, err := pickBindingFrom(in, &out, "acme", "slack", bindings(3))
	if err == nil {
		t.Fatal("invalid input should error")
	}
}

func TestPickBindingFrom_RendersLabels(t *testing.T) {
	in := strings.NewReader("1\n")
	var out bytes.Buffer
	bs := []channels.ChannelBinding{
		{Platform: "slack", ID: "C1", Label: "#legal"},
		{Platform: "slack", ID: "C2", Label: "#sales"},
	}
	_, _, err := pickBindingFrom(in, &out, "contracts", "slack", bs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		`contracts`,
		`2 slack bindings`,
		`[1] slack:C1 (#legal)`,
		`[2] slack:C2 (#sales)`,
		`[a] all`,
		`Which to remove?`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, got)
		}
	}
}

func TestPickBindingFrom_LabelsOptional(t *testing.T) {
	// Bindings without labels should not add empty parens.
	in := strings.NewReader("1\n")
	var out bytes.Buffer
	bs := []channels.ChannelBinding{
		{Platform: "slack", ID: "C1"},
	}
	_, _, err := pickBindingFrom(in, &out, "ada", "slack", bs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "()") {
		t.Errorf("unlabeled binding should not render empty parens, got:\n%s", got)
	}
}

func TestSplitPlatformID(t *testing.T) {
	cases := []struct {
		in, wantP, wantID string
	}{
		{"slack", "slack", ""},
		{"slack:C1", "slack", "C1"},
		{"slack:C0123456789", "slack", "C0123456789"},
		{"telegram:-1001234", "telegram", "-1001234"},
		{"", "", ""},
	}
	for _, c := range cases {
		p, id := splitPlatformID(c.in)
		if p != c.wantP || id != c.wantID {
			t.Errorf("splitPlatformID(%q) = (%q, %q), want (%q, %q)",
				c.in, p, id, c.wantP, c.wantID)
		}
	}
}

func TestFormatAgentChannels(t *testing.T) {
	cases := []struct {
		name string
		in   []channels.ChannelBinding
		want string
	}{
		{
			name: "gateway-only",
			in:   nil,
			want: "(gateway-only)",
		},
		{
			name: "single binding",
			in: []channels.ChannelBinding{
				{Platform: "slack", ID: "U0123"},
			},
			want: "slack:U0123",
		},
		{
			name: "three short slack bindings stays inline",
			in: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1"},
				{Platform: "slack", ID: "C2"},
				{Platform: "slack", ID: "C3"},
			},
			want: "slack:C1,C2,C3",
		},
		{
			name: "many long slack bindings collapses to count",
			in: []channels.ChannelBinding{
				{Platform: "slack", ID: "C0123456789"},
				{Platform: "slack", ID: "C0234567890"},
				{Platform: "slack", ID: "C0345678901"},
				{Platform: "slack", ID: "C0456789012"},
				{Platform: "slack", ID: "C0567890123"},
			},
			want: "slack (5)",
		},
		{
			name: "different platforms separated by semicolons",
			in: []channels.ChannelBinding{
				{Platform: "slack", ID: "C1"},
				{Platform: "telegram", ID: "100"},
			},
			want: "slack:C1; telegram:100",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatAgentChannels(provider.AgentConfig{Channels: c.in})
			if got != c.want {
				t.Errorf("formatAgentChannels() = %q, want %q", got, c.want)
			}
		})
	}
}

// Formatting for the ambiguous-unbind error is now provided by
// pkg/provider.FormatAmbiguousUnbindError; see pkg/provider/bind_test.go
// for the exhaustive message-content tests. This test stays only as a
// smoke check that the CLI imports the shared helper correctly.
func TestProviderFormatAmbiguousUnbindError_Wiring(t *testing.T) {
	bs := []channels.ChannelBinding{
		{Platform: "slack", ID: "C1", Label: "#legal"},
	}
	err := provider.FormatAmbiguousUnbindError("acme", "slack", bs)
	if err == nil || !strings.Contains(err.Error(), "slack:C1") {
		t.Errorf("shared helper not wired through: %v", err)
	}
}
