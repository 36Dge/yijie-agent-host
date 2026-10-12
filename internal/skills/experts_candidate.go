package skills

// ReviewedExpertCatalogDigest fixes the local canonical Skills candidate bytes.
// It is never supplied by a renderer, HTTP request or arbitrary environment hash.
// Source authority: Desktop contracts/experts-assets.candidate.json and the
// matching yijie-skills canonical bundle; current jd-ops-expert content 0.1.3.
// This single reviewed local candidate is not release provenance or permission
// to replace an older installation still referenced by a retained conversation.
const ReviewedExpertCatalogDigest = "4fec45edbe0995d0fa809c11c1fb37189176c6e9fc7fa7e8cce6a31cf1eea201"

func VerifyExpertCandidate(root string) error {
	current, e := loadCatalog(root)
	if e != nil {
		return e
	}
	return validateExpertCatalogAuthority(current, ReviewedExpertCatalogDigest)
}
