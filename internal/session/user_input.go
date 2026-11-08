package session

import (
	"context"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	wire "github.com/36Dge/yijie-agent-host/internal/contracts/userinput"
)

type userInputRuntime interface {
	UserInputEnabled() bool
	UserInputSnapshot(string) wire.UserInputSnapshot
	GetUserInput(string, string) (wire.UserInputRequest, error)
	TouchUserInput(string, string) (wire.UserInputRequest, error)
	AnswerUserInput(context.Context, string, string, wire.UserInputAnswerRequest) (wire.UserInputRequest, error)
}

func (s *Service) userInputSession(sessionID string) (userInputRuntime, Record, error) {
	record, err := s.GetSession(sessionID)
	if err != nil {
		return nil, record, err
	}
	if record.Purpose != purposeOrdinary && record.Purpose != purposeMarket {
		return nil, record, ErrSessionNotUsable
	}
	runtime, ok := s.runtime.(userInputRuntime)
	if !ok || !runtime.UserInputEnabled() {
		return nil, record, ErrSessionNotUsable
	}
	return runtime, record, nil
}
func (s *Service) UserInputSnapshot(sessionID string) (wire.UserInputSnapshot, error) {
	runtime, record, err := s.userInputSession(sessionID)
	if err != nil {
		return wire.UserInputSnapshot{}, err
	}
	return runtime.UserInputSnapshot(record.CodexThreadID), nil
}
func (s *Service) GetUserInput(sessionID, id string) (wire.UserInputRequest, error) {
	runtime, record, err := s.userInputSession(sessionID)
	if err != nil {
		return wire.UserInputRequest{}, err
	}
	if !validUUID(id) || record.CodexThreadID == "" {
		return wire.UserInputRequest{}, codex.ErrUserInputNotFound
	}
	return runtime.GetUserInput(record.CodexThreadID, id)
}
func (s *Service) TouchUserInput(sessionID, id string) (wire.UserInputRequest, error) {
	runtime, record, err := s.userInputSession(sessionID)
	if err != nil {
		return wire.UserInputRequest{}, err
	}
	if !validUUID(id) || record.CodexThreadID == "" {
		return wire.UserInputRequest{}, codex.ErrUserInputNotFound
	}
	return runtime.TouchUserInput(record.CodexThreadID, id)
}
func (s *Service) AnswerUserInput(ctx context.Context, sessionID, id string, answer wire.UserInputAnswerRequest) (wire.UserInputRequest, error) {
	runtime, record, err := s.userInputSession(sessionID)
	if err != nil {
		return wire.UserInputRequest{}, err
	}
	if !validUUID(id) || record.CodexThreadID == "" {
		return wire.UserInputRequest{}, codex.ErrUserInputNotFound
	}
	return runtime.AnswerUserInput(ctx, record.CodexThreadID, id, answer)
}
