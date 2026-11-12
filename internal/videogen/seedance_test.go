package videogen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/videogeneration"
)

func TestSeedanceUsesDefaultBodyAndSeparateVideoDownload(t *testing.T) {
	raw, err := os.ReadFile("../session/fixtures/synthetic-video-16x16.mp4.base64")
	if err != nil {
		t.Fatal(err)
	}
	clip, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	var creates, queries, downloads atomic.Int64
	var base string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tasks":
			creates.Add(1)
			if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer ordinary-operator-key" {
				t.Error("wrong create authority")
			}
			var body map[string]json.RawMessage
			if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 2 || body["model"] == nil || body["content"] == nil {
				t.Error("provider defaults were overridden")
			}
			var model string
			_ = json.Unmarshal(body["model"], &model)
			if model != wire.Model {
				t.Error("wrong model")
			}
			_, _ = io.WriteString(w, `{"id":"cgt-normal-video"}`)
		case "/tasks/cgt-normal-video":
			queries.Add(1)
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer ordinary-operator-key" {
				t.Error("wrong query authority")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "cgt-normal-video", "model": wire.Model, "status": "succeeded", "content": map[string]any{"video_url": base + "/clip.mp4"}})
		case "/clip.mp4":
			downloads.Add(1)
			if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
				t.Error("provider credential escaped to media download")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(clip)
		default:
			t.Error("unexpected route")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	base = server.URL
	c := NewSeedanceClient(func(context.Context) (string, error) { return "ordinary-operator-key", nil })
	c.endpoint = base + "/tasks"
	c.http = server.Client()
	c.allowResultURL = func(u string) bool { return u == base+"/clip.mp4" }
	task, err := c.Create(context.Background(), Request{Prompt: "A ceramic cup on a table"})
	if err != nil {
		t.Fatal(err)
	}
	task, err = c.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := c.Download(context.Background(), task)
	if err != nil || string(result.Bytes) != string(clip) {
		t.Fatal("video bytes changed", err)
	}
	if creates.Load() != 1 || queries.Load() != 1 || downloads.Load() != 1 {
		t.Fatal("unexpected provider dispatch")
	}
}

func TestSeedanceNeverRetriesAnUncertainCreate(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
	defer server.Close()
	c := NewSeedanceClient(func(context.Context) (string, error) { return "ordinary-operator-key", nil })
	c.endpoint = server.URL
	c.http = server.Client()
	if _, err := c.Create(context.Background(), Request{Prompt: "A ceramic cup"}); Kind(err) != Unavailable {
		t.Fatal("unexpected outcome", err)
	}
	if calls.Load() != 1 {
		t.Fatal("uncertain create was retried")
	}
}

func TestSeedancePictureRolesDoNotAddGenerationOptions(t *testing.T) {
	for _, images := range [][]Reference{nil, {{DataURL: "data:image/png;base64,normal", Role: "first_frame"}}, {{DataURL: "data:image/png;base64,normal", Role: "first_frame"}, {DataURL: "data:image/png;base64,normal", Role: "last_frame"}}, {{DataURL: "data:image/png;base64,normal", Role: "reference_image"}}} {
		body, err := payload(Request{Prompt: "A ceramic cup", Images: images})
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]json.RawMessage
		_ = json.Unmarshal(body, &parsed)
		if len(parsed) != 2 {
			t.Fatal("optional provider parameters sent")
		}
		var parts []contentPart
		_ = json.Unmarshal(parsed["content"], &parts)
		if len(parts) != len(images)+1 {
			t.Fatal("reference role lost")
		}
		for i, image := range images {
			if parts[i+1].Role != image.Role {
				t.Fatal("reference role changed")
			}
		}
	}
}

func TestSeedanceCancelledBeforeCallDoesNotReadCredential(t *testing.T) {
	called := false
	c := NewSeedanceClient(func(context.Context) (string, error) { called = true; return "", nil })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Create(ctx, Request{Prompt: "A cup"})
	if Kind(err) != Cancelled || called {
		t.Fatal("cancelled request accessed credential")
	}
}

func TestSeedanceRetryAfterUsesBothHTTPForms(t *testing.T) {
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{{"30", 30 * time.Second}, {"7200", 2 * time.Hour}, {now.Add(time.Minute).Format(http.TimeFormat), time.Minute}, {now.Add(-time.Minute).Format(http.TimeFormat), 0}, {"", 0}} {
		if got := retryAfterHeader(tc.value, now); got != tc.want {
			t.Fatalf("Retry-After delay %v, want %v", got, tc.want)
		}
	}
}

func TestSeedanceDownloadRetainsRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Authorization") != "" {
			t.Error("unexpected download authority")
		}
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	c := NewSeedanceClient(func(context.Context) (string, error) { t.Error("download requested Ark key"); return "", nil })
	c.http = server.Client()
	c.allowResultURL = func(value string) bool { return value == server.URL+"/clip.mp4" }
	_, err := c.Download(context.Background(), Task{ID: "cgt-normal-download", Status: wire.ProviderStatusSucceeded, VideoURL: server.URL + "/clip.mp4"})
	if Kind(err) != Unavailable || RetryAfter(err) != 30*time.Second {
		t.Fatal("download rate limit lost its retry deadline", Kind(err), RetryAfter(err))
	}
}

func TestSeedanceQueryBodyDeadlineRemainsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c := NewSeedanceClient(func(context.Context) (string, error) { return "ordinary-loopback-key", nil })
	c.endpoint = server.URL + "/tasks"
	c.http = server.Client()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := c.Get(ctx, "cgt-normal-query")
	if Kind(err) != Unavailable {
		t.Fatal("read deadline was mislabeled as invalid provider JSON", Kind(err))
	}
}
