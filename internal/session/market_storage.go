package session

import (
	"context"
	"encoding/hex"
	"errors"

	bolt "go.etcd.io/bbolt"
)

const purposeMarket = "market_connectors"

var ErrMarketPurpose = errors.New("market session requires Native-authorized versioned delivery")
var marketOperationsBucket = []byte("market_operations_v1")

type marketSubmissionContextKey struct{}

func withMarketSubmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, marketSubmissionContextKey{}, true)
}
func isMarketSubmission(ctx context.Context) bool {
	v, _ := ctx.Value(marketSubmissionContextKey{}).(bool)
	return v
}

func WithMarketStorage() StoreOption {
	return func(s *Store) error { s.marketWriter, s.modelWriter, s.draftWriter = true, true, true; return nil }
}

func (s *Store) prepareMarketFormat(tx *bolt.Tx) error {
	v := domainStoreVersion(tx)
	if v == "8" || v == "9" || v == "10" {
		if tx.Bucket(marketOperationsBucket) == nil {
			return ErrMarketPurpose
		}
		return nil
	}
	if !s.marketWriter {
		return nil
	}
	if v != "7" || s.feat126ProjectDirectory != "" {
		return ErrMarketPurpose
	}
	if _, err := tx.CreateBucket(marketOperationsBucket); err != nil {
		return err
	}
	return putDomainStoreVersion(tx, "8")
}

func validateMarketRecord(record Record, version string) error {
	if record.Purpose != purposeMarket {
		if record.MarketScopeHash != "" || record.MarketLocalSessionID != "" {
			return ErrMarketPurpose
		}
		return nil
	}
	digest, err := hex.DecodeString(record.MarketScopeHash)
	if (version != "8" && version != "9" && version != "10") || err != nil || len(digest) != 32 || hex.EncodeToString(digest) != record.MarketScopeHash || !isCanonicalNonZeroUUID(record.MarketLocalSessionID) {
		return ErrMarketPurpose
	}
	if record.DraftWorkspaceID != "" || record.DraftSchemaVersion != 0 || record.DraftPolicyVersion != 0 {
		return ErrMarketPurpose
	}
	return nil
}
