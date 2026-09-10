package session

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/36Dge/yijie-agent-host/internal/codex"
)

type runtimePermissions interface {
	ListRuntimeApprovals(string) []codex.RuntimeApproval
	DecideRuntimeApproval(context.Context, string, string, string) (codex.RuntimeApproval, error)
}

func (s *Service) PrepareMcpPermissionScope(ctx context.Context, mode codex.PermissionMode) (bool, bool, error) {
	runtime, ok := s.runtime.(interface {
		PrepareMcpPermissionScope(context.Context, codex.PermissionMode) (bool, bool, error)
	})
	if !ok {
		return false, false, ErrSessionNotUsable
	}
	return runtime.PrepareMcpPermissionScope(ctx, mode)
}

func (s *Service) ListRuntimeApprovals(sessionID string) ([]codex.RuntimeApproval, error) {
	record, err := s.GetSession(sessionID)
	if err != nil {
		return nil, err
	}
	runtime, ok := s.runtime.(runtimePermissions)
	if !ok {
		return nil, ErrSessionNotUsable
	}
	requests := runtime.ListRuntimeApprovals(record.CodexThreadID)
	for i := range requests {
		requests[i] = safeRuntimeApproval(requests[i])
	}
	return requests, nil
}

func (s *Service) DecideRuntimeApproval(ctx context.Context, sessionID, id, decision string) (codex.RuntimeApproval, error) {
	record, err := s.GetSession(sessionID)
	if err != nil {
		return codex.RuntimeApproval{}, err
	}
	if !validUUID(id) {
		return codex.RuntimeApproval{}, ErrInvalidArgument
	}
	runtime, ok := s.runtime.(runtimePermissions)
	if !ok {
		return codex.RuntimeApproval{}, ErrSessionNotUsable
	}
	result, err := runtime.DecideRuntimeApproval(ctx, record.CodexThreadID, id, decision)
	if err != nil {
		return codex.RuntimeApproval{}, errors.Join(ErrSessionNotUsable, errors.New("runtime approval unavailable"))
	}
	return safeRuntimeApproval(result), nil
}

func safeRuntimeApproval(value codex.RuntimeApproval) codex.RuntimeApproval {
	clean := func(s string) string {
		// Approval is an operation review, not a log. Preserve ordinary paths
		// and URLs so the user can identify the target; retain credential guards.
		s = normalizeV5CommandText(s)
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if bearerPattern.MatchString(line) || v5SensitiveNamePattern.MatchString(line) ||
				v5JWTLikePattern.MatchString(line) || v5AWSAccessKeyPattern.MatchString(line) ||
				v5PEMBlockPattern.MatchString(line) {
				lines[i] = v5RedactedMarker
			}
		}
		s = strings.Join(lines, "\n")
		if len(s) > 4096 {
			s = s[:4096]
			for !utf8.ValidString(s) {
				s = s[:len(s)-1]
			}
		}
		return s
	}
	value.Summary = clean(value.Summary)
	value.Scope = clean(value.Scope)
	value.Reason = clean(value.Reason)
	return value
}

func (s *Service) requireNoRuntimeApproval(sessionID string) error {
	record, err := s.store.Get(sessionID)
	if err != nil {
		return err
	}
	return s.requireNoRuntimeApprovalForThread(record.CodexThreadID)
}

func (s *Service) requireNoRuntimeApprovalForThread(threadID string) error {
	runtime, ok := s.runtime.(runtimePermissions)
	if !ok {
		return nil
	}
	for _, request := range runtime.ListRuntimeApprovals(threadID) {
		if request.Status == "pending" {
			return ErrTurnActive
		}
	}
	return nil
}
