// Generated from document-runtime/v1; DO NOT EDIT.
package documentruntime

type EnvironmentInput struct {
	Format *string `json:"format,omitempty"`
}
type StatusInput struct {
	ArtifactId string `json:"artifact_id"`
}
type EnvironmentRequest struct {
	ThreadId string           `json:"thread_id"`
	TurnId   string           `json:"turn_id"`
	Input    EnvironmentInput `json:"input"`
}
type StatusRequest struct {
	ThreadId string      `json:"thread_id"`
	TurnId   string      `json:"turn_id"`
	Input    StatusInput `json:"input"`
}
type Guide struct {
	Format string `json:"format"`
	Path   string `json:"path"`
}
type Limits struct {
	GeneratedFileBytes     int64 `json:"generated_file_bytes"`
	TextSourceBytes        int64 `json:"text_source_bytes"`
	MaxPages               int64 `json:"max_pages"`
	MaxSheets              int64 `json:"max_sheets"`
	MaxCells               int64 `json:"max_cells"`
	MaxVisualTiles         int64 `json:"max_visual_tiles"`
	MaxArtifacts           int64 `json:"max_artifacts"`
	StepSoftTimeoutSeconds int64 `json:"step_soft_timeout_seconds"`
	JobSoftTimeoutSeconds  int64 `json:"job_soft_timeout_seconds"`
}
type EnvironmentResponse struct {
	SchemaVersion int64   `json:"schema_version"`
	Status        string  `json:"status"`
	Code          string  `json:"code"`
	BundleId      string  `json:"bundle_id"`
	Platform      string  `json:"platform"`
	PythonPath    string  `json:"python_path"`
	HelperPath    string  `json:"helper_path"`
	RendererPath  string  `json:"renderer_path"`
	Guides        []Guide `json:"guides"`
	Limits        Limits  `json:"limits"`
}
type StatusResponse struct {
	SchemaVersion int64  `json:"schema_version"`
	ArtifactId    string `json:"artifact_id"`
	Status        string `json:"status"`
}
type BundleFile struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	Sha256     string `json:"sha256"`
	SizeBytes  int64  `json:"size_bytes"`
	LinkTarget string `json:"link_target"`
}
type BundleManifest struct {
	SchemaVersion      int64        `json:"schema_version"`
	BundleId           string       `json:"bundle_id"`
	Platform           string       `json:"platform"`
	MinimumMacos       string       `json:"minimum_macos"`
	PythonVersion      string       `json:"python_version"`
	LibreofficeVersion string       `json:"libreoffice_version"`
	Python             string       `json:"python"`
	Helper             string       `json:"helper"`
	Renderer           string       `json:"renderer"`
	Libreoffice        string       `json:"libreoffice"`
	Fonts              []string     `json:"fonts"`
	Guides             []Guide      `json:"guides"`
	Limits             Limits       `json:"limits"`
	Files              []BundleFile `json:"files"`
}
type AdapterStartup struct {
	HostOrigin             string                  `json:"host_origin"`
	HostToken              string                  `json:"host_token"`
	Capability             string                  `json:"capability"`
	DocumentRuntimeEnabled *bool                   `json:"document_runtime_enabled,omitempty"`
	Media                  *MediaAdapterCapability `json:"media,omitempty"`
}
type InputReceipt struct {
	SchemaVersion int64  `json:"schema_version"`
	OperationId   string `json:"operation_id"`
	AttachmentId  string `json:"attachment_id"`
	Sha256        string `json:"sha256"`
	SizeBytes     int64  `json:"size_bytes"`
	Format        string `json:"format"`
	ExpiresAt     int64  `json:"expires_at"`
}
type MediaAdapterCapability struct {
	Capability string `json:"capability"`
	HostToken  string `json:"host_token"`
}

func FormatAllowed(value string) bool {
	switch value {
	case "txt", "md", "html", "csv", "json", "docx", "pptx", "xlsx", "pdf":
		return true
	}
	return false
}
