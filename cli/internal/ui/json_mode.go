package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

var (
	// JSONInputActive is true when --json was provided with data.
	JSONInputActive bool

	// OutputJSON is true when output should be JSON (--output json or implied by --json).
	OutputJSON bool

	// jsonData holds the parsed input object.
	jsonData map[string]any
)

// SetJSONMode parses JSON input and activates JSON mode.
func SetJSONMode(input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	var raw []byte
	if strings.HasPrefix(input, "@") {
		var err error
		raw, err = os.ReadFile(input[1:])
		if err != nil {
			return fmt.Errorf("reading JSON file %s: %w", input[1:], err)
		}
	} else {
		raw = []byte(input)
	}

	jsonData = make(map[string]any)
	if err := json.Unmarshal(raw, &jsonData); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	JSONInputActive = true
	OutputJSON = true
	return nil
}

// ResetJSONMode clears JSON mode state. Used in tests.
func ResetJSONMode() {
	JSONInputActive = false
	OutputJSON = false
	jsonData = nil
}

// JSONData returns the raw parsed JSON input map.
func JSONData() map[string]any {
	return jsonData
}

// GetString returns a string value from JSON input.
func GetString(key string) (string, bool) {
	if jsonData == nil {
		return "", false
	}
	v, ok := jsonData[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	return s, true
}

// GetInt returns an int value from JSON input.
func GetInt(key string) (int, bool) {
	if jsonData == nil {
		return 0, false
	}
	v, ok := jsonData[key]
	if !ok {
		return 0, false
	}
	// JSON numbers decode as float64
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int(f), true
}

// GetBool returns a bool value from JSON input.
func GetBool(key string) (bool, bool) {
	if jsonData == nil {
		return false, false
	}
	v, ok := jsonData[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	if !ok {
		return false, false
	}
	return b, true
}

// MustGetString returns a string from JSON input or an error if missing.
func MustGetString(key string) (string, error) {
	s, ok := GetString(key)
	if !ok {
		return "", fmt.Errorf("missing required JSON field: %q", key)
	}
	return s, nil
}

// TextPromptJ is the JSON-aware version of TextPrompt.
// In JSON mode, reads from JSON input. In text mode, falls through to interactive prompt.
func TextPromptJ(key, label string) (string, error) {
	if JSONInputActive {
		return MustGetString(key)
	}
	return TextPrompt(label)
}

// TextPromptWithDefaultJ is the JSON-aware version with a default value.
func TextPromptWithDefaultJ(key, label, defaultVal string) (string, error) {
	if JSONInputActive {
		if s, ok := GetString(key); ok {
			return s, nil
		}
		return defaultVal, nil
	}
	return TextPromptWithDefault(label, defaultVal)
}

// SecretPromptJ is the JSON-aware version of SecretPrompt.
func SecretPromptJ(key, label string) (string, error) {
	if JSONInputActive {
		return MustGetString(key)
	}
	return SecretPrompt(label)
}

// ConfirmJ returns true in JSON mode (like --force). In text mode, prompts.
func ConfirmJ(prompt string) bool {
	if JSONInputActive {
		return true
	}
	return Confirm(prompt)
}
