package session

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestFEAT157MarketStore8PreservesOrdinaryAndReadsWithFeatureOff(t *testing.T) {
	home := t.TempDir()
	s, err := OpenStore(home, WithChatModelStorage())
	if err != nil {
		t.Fatal(err)
	}
	ordinary := Record{TaskID: "11111111-1111-4111-8111-111111111111", AgentSessionID: "22222222-2222-4222-8222-222222222222", Cwd: home}
	if err := s.Reserve(ordinary); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(home, WithMarketStorage())
	if err != nil {
		t.Fatal(err)
	}
	if record, err := s.Get(ordinary.AgentSessionID); err != nil || record.Purpose != purposeOrdinary {
		t.Fatal("ordinary record not preserved")
	}
	market := Record{TaskID: "33333333-3333-4333-8333-333333333333", AgentSessionID: "44444444-4444-4444-8444-444444444444", Cwd: home, Purpose: purposeMarket, MarketScopeHash: strings.Repeat("a", 64), MarketLocalSessionID: "55555555-5555-4555-8555-555555555555"}
	if err := s.Reserve(market); err != nil {
		t.Fatal(err)
	}
	if _, err := s.update(market.AgentSessionID, func(record *Record) error { record.MarketLocalSessionID = ordinary.AgentSessionID; return nil }); err == nil {
		t.Fatal("market local identity was mutable")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(home, WithChatModelStorage()) // New reader, market writer off.
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if record, err := s.Get(market.AgentSessionID); err != nil || record.MarketLocalSessionID != market.MarketLocalSessionID {
		t.Fatal("market ownership was not preserved for reader")
	}
	if err := s.db.View(func(tx *bolt.Tx) error {
		if string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "8" {
			t.Fatal("model startup downgraded Store8")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	service := NewService(nil, s, NewEventHub(8, 8), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := service.StartTurnV2(context.Background(), StartTurnV2Input{AgentSessionID: market.AgentSessionID, OperationID: "66666666-6666-4666-8666-666666666666"}); !errors.Is(err, ErrMarketPurpose) {
		t.Fatalf("legacy turn reached market: %v", err)
	}
	if _, err := service.ResumeSession(context.Background(), market.AgentSessionID, TraceContext{}); !errors.Is(err, ErrMarketPurpose) {
		t.Fatalf("legacy resume reached market: %v", err)
	}
	market.AgentSessionID, market.TaskID = "77777777-7777-4777-8777-777777777777", "88888888-8888-4888-8888-888888888888"
	if err := s.Reserve(market); !errors.Is(err, ErrMarketPurpose) {
		t.Fatalf("feature-off market writer admitted: %v", err)
	}
}
