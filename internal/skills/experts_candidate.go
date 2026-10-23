package skills

// ReviewedExpertCatalogDigest fixes the local canonical Skills candidate bytes.
// It is never supplied by a renderer, HTTP request or arbitrary environment hash.
// Source authority: Desktop contracts/experts-assets.candidate.json and the
// matching yijie-skills canonical bundle: unchanged jd-ops-expert 0.1.3 plus
// xiaohongshu/comic/cross-border 0.1.0 retained, plus independent xiaohongshu
// and cross-border 0.1.1 installations for the factual-grounding repair.
// Also includes local quotation/report experts 0.1.0; all six older versions remain.
// Report 0.1.1 adds well-formed SVG validation; report 0.1.0 remains retained.
// TikTok 0.1.0 adds user-report and selected-connector guidance; no service authorization.
// Mannequin 0.1.0 adds public-image preparation and receipt inspection; no upload or live verification.
// Video replica 0.1.0 adds staged analysis/video request and receipt preparation on existing LinkFox.
// 1688 0.1.0 adds exact source matching and explicit contribution-cost scenarios on existing LinkFox.
// Industry research 0.1.0 adds current-connector research, cited comps and local HTML/XLSX artifacts.
// Product management and novel creation 0.1.0 add local versioned documents and continuity ledgers.
// All prior 52 resource entries and 14 expert versions remain exact.
// This single reviewed local candidate is not release provenance or permission
// to replace an older installation still referenced by a retained conversation.
// Data analysis and UI 0.1.0 append local EDA and interactive-page delivery.
// Data analysis 0.1.1 fixes narrow wrapping. Data0.1.2/UI0.1.1 prevent helper import caches.
// All prior 61 resources remain exact; no installation/history or integrity gates are replaced.
// FEAT-160 refreshes source/contract provenance only; all 75 resource entries and archives remain exact.
// 2026-10-21 developer read interface refreshes contract/source provenance only;
// all 75 resource entries and archive bytes are unchanged.
const ReviewedExpertCatalogDigest = "033689b005c2659fa625a7a8e93081ec04b2ffe7cfde67f0c293f69684874741"

func VerifyExpertCandidate(root string) error {
	current, e := loadCatalog(root)
	if e != nil {
		return e
	}
	return validateExpertCatalogAuthority(current, ReviewedExpertCatalogDigest)
}
