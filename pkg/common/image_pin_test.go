package common

import (
	"os"
	"regexp"
	"testing"
)

// TestOpenClawImagePinConsistency guards against the OpenClaw image pin drifting
// between the three places that declare it. A mismatch shipped once (the
// setup-manifest default lagged a minor behind the two var defaults) and was
// caught only in review — this locks them together so a future bump can't update
// one and forget the others.
//
// The setup-manifest default is the load-bearing one on AWS: it (not the `image`
// var) seeds /conga/config/image at `conga admin setup`, which is what a fresh
// host actually boots on. See ROADMAP.md (image-propagation gap).
//
// Paths are relative to this package dir (pkg/common); `go test` runs with the
// package directory as the working directory.
func TestOpenClawImagePinConsistency(t *testing.T) {
	files := []string{
		"../../terraform/environments/production/variables.tf", // env image var default
		"../../terraform/modules/congaline/variables.tf",       // congaline module image var default
		"../../terraform/modules/infrastructure/variables.tf",  // setup_manifest.defaults.image (AWS seed)
	}
	re := regexp.MustCompile(`ghcr\.io/openclaw/openclaw:([0-9]{4}\.[0-9]+\.[0-9]+)`)

	var pin, pinSource string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		m := re.FindStringSubmatch(string(b))
		if m == nil {
			t.Fatalf("no ghcr.io/openclaw/openclaw:<version> pin found in %s", f)
		}
		if pin == "" {
			pin, pinSource = m[1], f
			continue
		}
		if m[1] != pin {
			t.Errorf("OpenClaw image pin mismatch: %s has %s but %s has %s — all three must match", f, m[1], pinSource, pin)
		}
	}
}
