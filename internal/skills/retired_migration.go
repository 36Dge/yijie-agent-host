package skills

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

// Historical FEAT-129 ordinary identities use these five reserved namespaces.
// Expert resources use a separate namespace and must never be reclassified from
// a bare SKILL.md. Unknown receipt identities stop migration for explicit review.
func retiredOrdinaryCategory(id string) string {
	for _, category := range []string{"sourcing-selection", "market-research", "content-marketing", "traffic-advertising", "store-operations"} {
		if strings.HasPrefix(id, "yijie."+category+".") {
			return category
		}
	}
	return ""
}

// Supplement offers with receipt-validated local inventory. This does not make
// a removed asset installable, assert new redistribution rights, or parse YAML.
func (s *Service) includeRetiredOrdinary(current catalog) (catalog, error) {
	rows, err := fs.ReadDir(s.managed.FS(), ".")
	if err != nil {
		return catalog{}, err
	}
	for _, row := range rows {
		id := row.Name()
		offer, known := current.byID[id]
		if !validSkillID(id) || known && (isExpertResource(offer) || catalogEntryMode(offer) == "bundled" && offer.Entrypoint != "") {
			continue
		}
		if strings.HasPrefix(id, "yijie.experts.") {
			continue
		}
		root, err := s.managed.OpenRoot(id)
		if err != nil {
			continue
		}
		raw, err := root.ReadFile(receiptFileName)
		root.Close()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || len(raw) > maxManifestBytes {
			return catalog{}, errors.New("legacy receipt inventory unavailable")
		}
		category := retiredOrdinaryCategory(id)
		if known {
			category = offer.Category
		}
		var receipt installReceipt
		if category == "" || strictJSON(raw, &receipt) != nil || receipt.SkillID != id || !validRuntimeName(receipt.RuntimeName) {
			return catalog{}, errors.New("legacy receipt identity requires review")
		}
		skill := ManifestSkill{ID: id, RuntimeName: receipt.RuntimeName, Version: receipt.Version,
			Category: category, DisplayName: receipt.RuntimeName, Description: "旧版本保留的已安装技能", Entrypoint: "SKILL.md",
			CatalogEntryMode: "bundled", Release: Release{CatalogStatus: "blocked", MaintenanceStatus: "unmaintained", BlockedReason: "source_unverified"}, legacyRetired: true,
			Archive: Archive{SHA256: receipt.ArchiveSHA256, FileCount: len(receipt.Files)}}
		if known {
			skill.DisplayName, skill.Description = offer.DisplayName, offer.Description
		}
		for _, file := range receipt.Files {
			if file.Size < 0 || file.Size > (64<<20)-skill.Archive.UncompressedSizeBytes {
				return catalog{}, errors.New("legacy receipt inventory exceeds its bounds")
			}
			skill.Archive.UncompressedSizeBytes += file.Size
		}
		installed := inspectInstallation(s.managed, skill)
		if !installed.valid {
			return catalog{}, errorWithCode(installed.failure, errors.New("retired legacy installation requires review"))
		}
		current.byID[id] = skill
		if known {
			for i := range current.manifest.Skills {
				if current.manifest.Skills[i].ID == id {
					current.manifest.Skills[i] = skill
					break
				}
			}
		} else {
			current.manifest.Skills = append(current.manifest.Skills, skill)
		}
	}
	return current, nil
}
