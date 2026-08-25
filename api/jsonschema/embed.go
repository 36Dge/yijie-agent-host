// Package jsonschema embeds contract snapshots required by the Agent Host at runtime.
package jsonschema

import _ "embed"

// SkillBundleManifestV1 is the exact schema pinned in api/contracts.lock.
//
//go:embed skill-bundle-manifest-v1.schema.json
var SkillBundleManifestV1 []byte

// SkillBundleManifestV2 is the exact Catalog First schema pinned in
// api/contracts.lock. V1 remains embedded only for backwards-compatible reads
// while Desktop migrates; all FEAT-129 product bundles use V2.
//
//go:embed skill-bundle-manifest-v2.schema.json
var SkillBundleManifestV2 []byte
