package participant

import (
	"os"
	"testing"

	"csgclaw/internal/apitypes"
	"csgclaw/internal/config"
	"csgclaw/internal/localstore"
)

func TestConfiguredRootParticipantsPreserveAgentSection(t *testing.T) {
	t.Setenv("CSGCLAW_DATA_DIR", t.TempDir())
	path, err := config.DefaultStatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte(`{"version":1,"agents":{"items":[]}}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(apitypes.Participant{ID: "person-test", Channel: ChannelCSGClaw, Type: TypeHuman, Name: "Test"}); err != nil {
		t.Fatal(err)
	}
	var agents struct {
		Items []any `json:"items"`
	}
	found, err := localstore.ReadSection(path, "agents", &agents)
	if err != nil || !found {
		t.Fatalf("participants overwrote agents: %v %v", found, err)
	}
	restored, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.Get(ChannelCSGClaw, "person-test"); !ok {
		t.Fatal("participant missing after restart")
	}
}
