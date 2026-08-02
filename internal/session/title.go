package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
	"golang.org/x/text/unicode/norm"
)

var (
	ErrTitleOperationConflict = errors.New("title operation conflicts with another input")
	ErrTitleUnavailable       = errors.New("title generation is unavailable")
	ErrTitleOutputInvalid     = errors.New("title output is invalid")
)

type TitleGenerator interface {
	GenerateTitle(context.Context, string) (string, error)
}

type TitleResult struct {
	OperationID string
	Title       string
}

type titleOperation struct {
	inputSHA256 string
	result      TitleResult
	pending     bool
}

func WithTitleGenerator(generator TitleGenerator) ServiceOption {
	return func(service *Service) { service.titleGenerator = generator }
}

func (s *Service) GenerateTitle(ctx context.Context, sessionID, operationID, input string) (TitleResult, error) {
	if err := requireUUID("agent_session_id", sessionID); err != nil {
		return TitleResult{}, err
	}
	if err := requireUUID("operation_id", operationID); err != nil {
		return TitleResult{}, err
	}
	input = strings.TrimSpace(input)
	if input == "" || len([]byte(input)) > 8<<10 {
		return TitleResult{}, ErrInvalidArgument
	}
	if _, err := s.store.Get(sessionID); err != nil {
		return TitleResult{}, err
	}
	digestBytes := sha256.Sum256([]byte(input))
	digest := hex.EncodeToString(digestBytes[:])
	s.titleMu.Lock()
	if existing := s.titleOperations[operationID]; existing != nil {
		if existing.inputSHA256 != digest {
			s.titleMu.Unlock()
			return TitleResult{}, ErrTitleOperationConflict
		}
		if existing.pending {
			s.titleMu.Unlock()
			return TitleResult{}, ErrTitleUnavailable
		}
		result := existing.result
		s.titleMu.Unlock()
		return result, nil
	}
	s.titleOperations[operationID] = &titleOperation{inputSHA256: digest, pending: true}
	s.titleMu.Unlock()

	if s.titleGenerator == nil {
		s.finishTitleOperation(operationID, TitleResult{}, false)
		return TitleResult{}, ErrTitleUnavailable
	}
	rawTitle, err := s.titleGenerator.GenerateTitle(ctx, input)
	if err != nil {
		s.finishTitleOperation(operationID, TitleResult{}, false)
		return TitleResult{}, ErrTitleUnavailable
	}
	title, err := sanitizeGeneratedTitle(rawTitle)
	if err != nil {
		s.finishTitleOperation(operationID, TitleResult{}, false)
		return TitleResult{}, err
	}
	result := TitleResult{OperationID: operationID, Title: title}
	s.finishTitleOperation(operationID, result, true)
	return result, nil
}

func (s *Service) finishTitleOperation(operationID string, result TitleResult, keep bool) {
	s.titleMu.Lock()
	defer s.titleMu.Unlock()
	if !keep {
		delete(s.titleOperations, operationID)
		return
	}
	operation := s.titleOperations[operationID]
	if operation != nil {
		operation.pending = false
		operation.result = result
	}
}

func sanitizeGeneratedTitle(value string) (string, error) {
	value = norm.NFC.String(strings.TrimSpace(value))
	if value == "" || !norm.NFC.IsNormalString(value) || strings.ContainsAny(value, "\r\n<>#*_~`|[]{}") {
		return "", ErrTitleOutputInvalid
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character) {
			return "", ErrTitleOutputInvalid
		}
	}
	graphemes := uniseg.NewGraphemes(value)
	count := 0
	for graphemes.Next() {
		count++
	}
	if count < 1 || count > 40 {
		return "", ErrTitleOutputInvalid
	}
	return value, nil
}
