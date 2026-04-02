package terraform

import (
	"fmt"
	"testing"
)

func TestSplitImportID(t *testing.T) {
	tests := []struct {
		id       string
		n        int
		expected []string
	}{
		{"agent/secret", 2, []string{"agent", "secret"}},
		{"agent", 2, nil},
		{"agent/", 2, nil},
		{"/secret", 2, nil},
		{"", 2, nil},
		{"a/b/c", 2, []string{"a", "b/c"}},
		{"a/b/c", 3, []string{"a", "b", "c"}},
		{"single", 1, []string{"single"}},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s_n%d", tt.id, tt.n), func(t *testing.T) {
			result := splitImportID(tt.id, tt.n)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatalf("expected %v, got nil", tt.expected)
			}
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d parts, got %d", len(tt.expected), len(result))
			}
			for i, v := range tt.expected {
				if result[i] != v {
					t.Errorf("part %d: expected %q, got %q", i, v, result[i])
				}
			}
		})
	}
}

func TestIsNotFoundErr(t *testing.T) {
	tests := []struct {
		err      error
		expected bool
	}{
		{nil, false},
		{fmt.Errorf("agent not found"), true},
		{fmt.Errorf("No such container: conga-test"), true},
		{fmt.Errorf("resource does not exist"), true},
		{fmt.Errorf("connection timeout"), false},
		{fmt.Errorf("access denied"), false},
		{fmt.Errorf("Agent Not Found in store"), true},
	}

	for _, tt := range tests {
		name := "nil"
		if tt.err != nil {
			name = tt.err.Error()
		}
		t.Run(name, func(t *testing.T) {
			result := isNotFoundErr(tt.err)
			if result != tt.expected {
				t.Errorf("isNotFoundErr(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}
