// Generated from artifact/media-v2; DO NOT EDIT.
package artifactmedia

type PublishInput struct {
	RelativePath string `json:"relative_path"`
	DisplayName  string `json:"display_name"`
}
type PublishRequest struct {
	ThreadId string       `json:"thread_id"`
	TurnId   string       `json:"turn_id"`
	Input    PublishInput `json:"input"`
}
type PublishReceipt struct {
	ArtifactId string `json:"artifact_id"`
	Status     string `json:"status"`
}

func MediaAllowed(kind, mime string) bool {
	switch kind {
	case "image":
		switch mime {
		case "image/png", "image/jpeg", "image/webp", "image/svg+xml":
			return true
		}
	case "video":
		switch mime {
		case "video/mp4":
			return true
		}
	case "file":
		switch mime {
		case "text/plain", "text/csv", "application/json", "application/pdf", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "text/html", "text/markdown", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "application/vnd.openxmlformats-officedocument.presentationml.presentation":
			return true
		}
	case "report":
		switch mime {
		case "application/vnd.yijie.report+json;version=1":
			return true
		}
	}
	return false
}
func LegacyMediaAllowed(kind, mime string) bool {
	switch kind {
	case "image":
		switch mime {
		case "image/png", "image/jpeg", "image/webp", "image/svg+xml":
			return true
		}
	case "video":
		switch mime {
		case "video/mp4":
			return true
		}
	case "file":
		switch mime {
		case "text/plain", "text/csv", "application/json", "application/pdf", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "text/html":
			return true
		}
	case "report":
		switch mime {
		case "application/vnd.yijie.report+json;version=1":
			return true
		}
	}
	return false
}
