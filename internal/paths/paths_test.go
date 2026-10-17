package paths

import (
	"path/filepath"
	"testing"
)

// XDG_CONFIG_HOME and XDG_DATA_HOME win on every system, so a test or a
// user can point rflib anywhere.
func TestXDGWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	if got, err := ConfigDir(); err != nil || got != filepath.Join(dir, "config", "rflib") {
		t.Errorf("ConfigDir = %s, %v", got, err)
	}
	if got, err := DataDir(); err != nil || got != filepath.Join(dir, "data", "rflib") {
		t.Errorf("DataDir = %s, %v", got, err)
	}
}
