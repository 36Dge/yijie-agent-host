// Package jsonschema embeds contract snapshots required by the Agent Host at runtime.
package jsonschema

import _ "embed"

// SkillBundleManifestV1 is the exact schema pinned in api/contracts.lock.
//
//go:embed skill-bundle-manifest-v1.schema.json
var SkillBundleManifestV1 []byte
