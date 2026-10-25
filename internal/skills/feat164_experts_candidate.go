package skills

import (
	_ "embed"
	"encoding/json"
)

// Immutable Skills 595c927, canonical bundle 0.25.0. All 75 resource entries
// and archives are unchanged; FEAT-164 only fixes source provenance. No caller
// supplied digest and no replacement of an existing managed installation.
//
//go:embed feat164-expert-assets.json
var feat164ExpertAssets []byte

func ExpertCatalogDigestForProfile(kimiK31 bool) string {
	if !kimiK31 {
		return ReviewedExpertCatalogDigest
	}
	var pin struct {
		Manifest string `json:"manifest_sha256"`
	}
	if json.Unmarshal(feat164ExpertAssets, &pin) != nil {
		return ""
	}
	return pin.Manifest
}
func VerifyExpertCandidateForProfile(root string, kimiK31 bool) error {
	if !kimiK31 {
		return VerifyExpertCandidate(root)
	}
	current, err := loadCatalog(root)
	if err != nil {
		return err
	}
	return validateExpertCatalogAuthority(current, ExpertCatalogDigestForProfile(true))
}
