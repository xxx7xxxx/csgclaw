package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDirectoryOverrideIsolatesDefaultState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CSGCLAW_DATA_DIR", root)
	for name, resolve := range map[string]func() (string, error){"root": DefaultDir, "config": DefaultPath, "state": DefaultStatePath, "agents": DefaultAgentsDir, "im": DefaultIMDir, "sessions": DefaultAgentSessionBindingsDir, "auth-state": DefaultStatePath} {
		path, err := resolve()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			t.Fatalf("%s escaped isolated root: %s", name, path)
		}
	}
}
