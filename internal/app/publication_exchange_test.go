package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/36Dge/yijie-agent-host/internal/session"
)

// In the recorded-provider acceptance the Runtime has already executed. Only
// thread/read crosses this test boundary; the Host uses its real service,
// binding store, projector, event hub, authentication and HTTP/SSE writer.
type publicationHistory struct {
	session.Runtime
	thread json.RawMessage
}

func (publicationHistory) SupportsPublicMessagePublication() bool { return true }

func (r publicationHistory) ReadThread(context.Context, string) (json.RawMessage, error) {
	return r.thread, nil
}

func TestPublicMessagePublicationExchange(t *testing.T) {
	dir := os.Getenv("YIJIE_PUBLICATION_EXCHANGE")
	if dir == "" {
		t.Skip("requires local Runtime acceptance evidence")
	}
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	var input []struct {
		Method string
		Params json.RawMessage
	}
	if err := json.Unmarshal(read("runtime-events.json"), &input); err != nil {
		t.Fatal(err)
	}
	raw := read("runtime-thread.json")
	var identity struct{ ID string }
	if err := json.Unmarshal(raw, &identity); err != nil {
		t.Fatal(err)
	}
	store, err := session.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err = store.Reserve(session.Record{TaskID: feat137AppTaskID, AgentSessionID: feat137AppSessionID, Cwd: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.BindThread(feat137AppSessionID, identity.ID, "publication-replay", "", ""); err != nil {
		t.Fatal(err)
	}
	svc := session.NewService(publicationHistory{thread: raw}, store, session.NewEventHub(512, 64), nil)
	for _, e := range input {
		svc.HandleNotification(e.Method, e.Params)
	}
	mux := http.NewServeMux()
	const token = "local-publication-exchange"
	registerNativeConversation(mux, &sessionHandler{service: svc, apiToken: token})
	server := httptest.NewServer(mux)
	defer server.Close()
	client := &http.Client{Timeout: 15 * time.Second}
	get := func(path string, extended bool) *http.Response {
		r, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+token)
		if extended {
			r.Header.Set("X-Yijie-Publication", "2")
		}
		resp, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || extended && resp.Header.Get("X-Yijie-Publication") != "2" {
			resp.Body.Close()
			t.Fatalf("negotiation failed %d", resp.StatusCode)
		}
		return resp
	}
	path := "/v6/agent-sessions/" + feat137AppSessionID + "/native-thread"
	for _, extended := range []bool{false, true} {
		resp := get(path, extended)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(`"publication":`)) != extended {
			t.Fatal("incorrect history capability projection")
		}
		if extended {
			if err = os.WriteFile(filepath.Join(dir, "host-history.json"), body, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	stream := get("/v13/agent-sessions/"+feat137AppSessionID+"/events", true)
	defer stream.Body.Close()
	scanner := bufio.NewScanner(stream.Body)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	var frames bytes.Buffer
	var terminal bool
	for scanner.Scan() {
		line := scanner.Bytes()
		frames.Write(line)
		frames.WriteByte('\n')
		if bytes.HasPrefix(line, []byte("data: ")) {
			var event session.Event
			if err = json.Unmarshal(line[6:], &event); err != nil {
				t.Fatal(err)
			}
			if n := event.Payload.Native; n != nil && n.Method == "turn/completed" {
				terminal = true
			}
		}
		if terminal && len(line) == 0 {
			break
		}
	}
	if !terminal {
		t.Fatalf("missing SSE completion: %v", scanner.Err())
	}
	if err = os.WriteFile(filepath.Join(dir, "host.sse"), frames.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}
