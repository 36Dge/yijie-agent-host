package session

import (
	"encoding/json"
	native "github.com/36Dge/yijie-agent-host/internal/contracts/nativeconversationv6"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRecordedScopedSearch(t *testing.T) {
	dir := os.Getenv("SCOPED_REPLAY_DIRECTORY")
	if dir == "" {
		t.Skip("requires isolated recorded gateway outputs")
	}
	for _, name := range []string{"three", "kimi", "minimax"} {
		raw, err := os.ReadFile(filepath.Join(dir, "mcp-"+name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var calls []json.RawMessage
		if err = json.Unmarshal(raw, &calls); err != nil {
			t.Fatal(err)
		}
		var rows []native.NativeItem
		for _, call := range calls {
			row, err := projectNativeItemWithSearchDetails(call, "", false, true)
			if err != nil || row.Mcp == nil || row.Mcp.SearchReceipt == nil {
				t.Fatalf("%s lost receipt %v", name, err)
			}
			rows = append(rows, row)
		}
		data, _ := json.Marshal(rows)
		if err = os.WriteFile(filepath.Join(dir, "native-"+name+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
