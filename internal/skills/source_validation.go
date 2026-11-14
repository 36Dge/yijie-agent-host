package skills

// Source validation is shared with the native marketplace input adapter.
// Installing the resulting source remains exclusively a Runtime operation.
func ValidSourcePath(name string) bool      { return validArchiveEntryName(name) }
func SourceCollisionKey(name string) string { return collisionKey(name) }
