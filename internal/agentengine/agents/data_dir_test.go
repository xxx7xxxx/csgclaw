package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"csgclaw/internal/config"
	"csgclaw/internal/localstore"
)

func TestConfiguredDataRootPersistsRestartableAgentSection(t *testing.T) {
	root := filepath.Join(t.TempDir(), "isolated-instance")
	t.Setenv("CSGCLAW_DATA_DIR", root)
	path, err := config.DefaultStatePath()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewController(config.ModelConfig{}, config.ServerConfig{}, "test", path)
	if err != nil {
		t.Fatal(err)
	}
	svc.agents[ManagerUserID] = Agent{ID: ManagerUserID, Name: ManagerName, Role: RoleManager, RuntimeID: runtimeIDForAgentID(ManagerUserID), RuntimeKind: RuntimeKindCodex, CreatedAt: time.Now()}
	if err = svc.saveLocked(); err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	var section struct {
		Items []json.RawMessage `json:"items"`
	}
	if err = json.Unmarshal(document["agents"], &section); err != nil || len(section.Items) != 1 {
		t.Fatalf("agents were not saved as a root section: %s", document["agents"])
	}
	if err = localstore.MigrateMCPIdentities(path); err != nil {
		t.Fatalf("state cannot pass next startup migration: %v", err)
	}
	restored, err := NewController(config.ModelConfig{}, config.ServerConfig{}, "test", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.Agent(ManagerUserID); !ok {
		t.Fatal("agent lost on restart")
	}
	projects, err := ProjectsRoot()
	if err != nil || projects != filepath.Join(root, hostProjectsDir) {
		t.Fatalf("projects escaped data root: %s %v", projects, err)
	}
}
