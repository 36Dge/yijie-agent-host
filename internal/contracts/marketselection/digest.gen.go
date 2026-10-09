// Generated from market-selection source; DO NOT EDIT.
package marketselection

import (
	"crypto/sha256"
	"errors"
	"fmt"
	market "github.com/36Dge/yijie-agent-host/internal/contracts/marketconnectors"
	"sort"
	"strings"
)

// SelectionRef remains the existing generated authority, not a shadow DTO.
type SelectionRef = market.SelectionRef

func CanonicalSelectionBytes(turnOperationID string, refs []SelectionRef) ([]byte, error) {
	if err := market.CanonicalId(turnOperationID).Validate(); err != nil {
		return nil, err
	}
	if len(refs) > 58 {
		return nil, errors.New("too many connector references")
	}
	sorted := append([]SelectionRef(nil), refs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].InstallationId < sorted[j].InstallationId })
	var text strings.Builder
	fmt.Fprintf(&text, "yijie.market-selection/v1\n%s\n%d\n", turnOperationID, len(sorted))
	for i, r := range sorted {
		if err := r.Validate(); err != nil {
			return nil, err
		}
		if i > 0 && sorted[i-1].InstallationId == r.InstallationId {
			return nil, errors.New("duplicate connector installation")
		}
		fmt.Fprintf(&text, "%s %d %d\n", r.InstallationId, r.Revision, r.Generation)
	}
	return []byte(text.String()), nil
}
func SelectionDigest(turnOperationID string, refs []SelectionRef) (string, error) {
	b, err := CanonicalSelectionBytes(turnOperationID, refs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}
