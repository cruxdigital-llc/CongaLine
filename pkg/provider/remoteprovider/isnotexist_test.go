package remoteprovider

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"
)

// isNotExist is the linchpin of GetAgent / ReadProxyManifest dispatching:
// "the remote file genuinely doesn't exist" must produce provider.ErrNotFound,
// but any other SSH failure (auth, dial, permission, cat exit code) must
// fall through and surface the real cause. The dispatch logic only behaves
// correctly if isNotExist classifies inputs precisely — these cases pin
// down the contract so it doesn't drift.
func TestIsNotExist(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// SFTP path: sftp wraps fs.ErrNotExist on a missing file open.
		{"fs.ErrNotExist", fs.ErrNotExist, true},
		{"os.ErrNotExist", os.ErrNotExist, true},
		{"wrapped fs.ErrNotExist", fmt.Errorf("opening remote file: %w", fs.ErrNotExist), true},

		// cat fallback path: shell stderr variants observed in practice.
		{"cat 'No such file or directory'", fmt.Errorf("cat: /opt/conga/agents/x.json: No such file or directory"), true},
		{"lowercase 'no such file'", fmt.Errorf("no such file"), true},
		{"sftp 'file does not exist'", fmt.Errorf("file does not exist"), true},

		// Must NOT classify as not-exist:
		{"nil", nil, false},
		{"permission denied", fmt.Errorf("permission denied"), false},
		{"ssh dial timeout", fmt.Errorf("ssh: handshake failed: dial tcp: i/o timeout"), false},
		{"auth failure", fmt.Errorf("ssh: unable to authenticate, attempted methods [none publickey]"), false},
		{"network unreachable", fmt.Errorf("dial tcp: connect: network is unreachable"), false},
		// Critical: the docstring on isNotExist warns about this — a bare
		// "command not found" from a missing `cat` (exit 127) or other
		// shell failure must NOT be silently rebranded as a missing agent.
		{"cat: command not found", fmt.Errorf("cat: command not found"), false},
		{"bare 'not found'", fmt.Errorf("not found"), false},
		{"generic error", errors.New("boom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotExist(tt.err)
			if got != tt.want {
				t.Errorf("isNotExist(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
