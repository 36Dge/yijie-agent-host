package session

import (
	"context"

	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthost "github.com/36Dge/yijie-agent-host/internal/contracts"
	bolt "go.etcd.io/bbolt"
)

// Schema 12 extends the existing model-v2 envelope so an old Host cannot
// silently restore a light session with its full default feature configuration.
func WithToolProfileStorage() StoreOption {
	return func(s *Store) error { s.toolProfileWriter = true; return nil }
}

func (s *Store) prepareToolProfileFormat(tx *bolt.Tx) error {
	if !s.toolProfileWriter {
		return nil
	}
	if !s.modelV2Writer || string(tx.Bucket(metadataBucket).Get(storeSchemaVersionKey)) != "11" {
		return ErrModelUnavailable
	}
	return tx.Bucket(metadataBucket).Put(storeSchemaVersionKey, []byte("12"))
}

func normalizeToolProfile(profile agenthost.ToolProfile) (agenthost.ToolProfile, error) {
	switch profile {
	case "", "full":
		return "", nil // Preserve the exact legacy record representation.
	case "light":
		return profile, nil
	default:
		return "", ErrInvalidArgument
	}
}

func (s *Store) validateStoredToolProfile(record Record) error {
	if record.ToolProfile == "" {
		return nil
	}
	if record.ToolProfile != "light" || !s.toolProfileWriter || record.Purpose == purposeDraft {
		return ErrModelUnavailable
	}
	return nil
}

func (s *Service) contextForToolProfile(ctx context.Context, profile agenthost.ToolProfile) (context.Context, error) {
	profile, err := normalizeToolProfile(profile)
	if err != nil {
		return nil, err
	}
	if profile != "" {
		runtime, ok := s.runtime.(interface{ ToolProfilesEnabled() bool })
		if !s.store.toolProfileWriter || !ok || !runtime.ToolProfilesEnabled() {
			return nil, ErrModelUnavailable
		}
	}
	return codex.WithToolProfile(ctx, profile), nil
}
