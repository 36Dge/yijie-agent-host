package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	wire "github.com/36Dge/yijie-agent-host/internal/contracts/skillmarketv2"
	"github.com/google/uuid"
)

type skillMarketHTTPProbe struct {
	NativeSkillMarket
	calls    int
	received wire.ImportRequest
	data     []byte
}

func (p *skillMarketHTTPProbe) Catalog(context.Context) (wire.Catalog, error) {
	p.calls++
	return wire.Catalog{SchemaVersion: 2, Revision: string(bytes.Repeat([]byte("a"), 64)), Skills: []wire.Skill{}, Operations: []wire.PendingOperation{}}, nil
}
func (p *skillMarketHTTPProbe) Import(_ context.Context, request wire.ImportRequest, data []byte) (wire.Operation, error) {
	p.calls++
	p.received = request
	p.data = append([]byte(nil), data...)
	return wire.Operation{OperationId: request.OperationId, SkillId: "ordinary", Epoch: 1, State: wire.OperationStatePending}, nil
}

func TestFEAT174HTTPImportUsesVersionedBytesAndOwnerBoundary(t *testing.T) {
	probe := &skillMarketHTTPProbe{}
	mux := http.NewServeMux()
	registerNativeSkillRoutes(mux, SkillFeatureConfig{Read: true, Manage: true, NativeMarketEnabled: true}, probe, "ordinary-local-owner-token")
	request := httptest.NewRequest(http.MethodGet, "/v2/skills", nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != 401 || probe.calls != 0 {
		t.Fatal("owner boundary bypassed")
	}
	data := []byte("---\nname: ordinary\ndescription: A normal skill.\n---\nOrdinary text.\n")
	hash := sha256.Sum256(data)
	id := uuid.NewString()
	request = httptest.NewRequest(http.MethodPost, "/v2/skills/imports/"+id+"?filename=SKILL.md", bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer ordinary-local-owner-token")
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("X-Skill-SHA256", hex.EncodeToString(hash[:]))
	recorder = httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	if recorder.Code != 200 || probe.received.OperationId != id || probe.received.Format != "markdown" || probe.received.Bytes != int64(len(data)) || !bytes.Equal(probe.data, data) {
		t.Fatal("versioned import envelope differs", recorder.Code)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("private operation response can be cached")
	}
}
