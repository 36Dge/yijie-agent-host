package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	BaselineName                    = "Runtime Baseline 0"
	ExpectedSchemaVersion           = 1
	ExpectedUpstreamURL             = "https://github.com/openai/codex.git"
	ExpectedUpstreamTag             = "rust-v0.144.6"
	ExpectedUpstreamCommit          = "5d1fbf26c43abc65a203928b2e31561cb039e06d"
	ExpectedRuntimeRepositoryCommit = "9ed24710d73f22a9b269092b8cdf2225199ea222"
	ExpectedRuntimeRepositoryTree   = "984e0f5bb48aaa953ed3a329614d00e5905514fb"
	ExpectedRuntimeVersion          = "0.144.6"
	ExpectedReportedVersion         = "codex-cli 0.144.6"
	ExpectedRustToolchain           = "1.95.0"
	ExpectedTarget                  = "aarch64-apple-darwin"
	ExpectedTransport               = "stdio"
	ExpectedRuntimeSHA256           = "896d303658a0978c3628f10e9e78f12139168be9508dd5f2abc658db186a828b"
	ExpectedRuntimeSize             = int64(355996616)
	ExpectedRuntimeManifestSHA256   = "c428c0d06c9cf578e4bcfe53b328015977578469fc46e9461cb85f8c8bcd66fb"
	ExpectedRuntimePatch1Path       = ".yijie/patches/0001-feat-126-filter-persistent-diagnostics.patch"
	ExpectedRuntimePatch1SHA256     = "6b337a02caf064c6819fab5c7367a485004c85cce0d42acb06fa6d5003e599a0"
	ExpectedRuntimePatch2Path       = ".yijie/patches/0002-feat-136-unified-exec-pre-emitter-command-lifecycle.patch"
	ExpectedRuntimePatch2SHA256     = "43de168e1443f4b9ca60d7f61e3de2daf20e1cfea14d2e196d28ba417bf3e06d"
	ExpectedRuntimePatch3Path       = ".yijie/patches/0003-feat-137-stable-sandbox-provenance.patch"
	ExpectedRuntimePatch3SHA256     = "af7196f609fbbe722f69e7913d2aeb2f38bfc5f4cbed4bfb32c9e6f844a9910c"
	ExpectedRuntimePatch4Path       = ".yijie/patches/0004-feat-137-deterministic-approval-producer.patch"
	ExpectedRuntimePatch4SHA256     = "b66583db09948fda038eaf056310d6116a37930e8b4d08e73d483c7ed1cf74e6"
	ExpectedSchemaTreeSHA256        = "d82a33f683e554c10dd056a0101c26fd24477928e3f98ee3d9ef250b97395228"
	ExpectedResolvedLockSHA256      = "5cc77d7dfcc2828d3d389daf5824998c445c01e1d30367b04885813242d53f11"
	ExpectedUpstreamLockSHA256      = "175793a40a3147db1fee08fd9db0acc59312c344b3513dd7ee316f5446d8119e"
)

type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	Baseline      string            `json:"baseline"`
	RustToolchain string            `json:"rustToolchain"`
	Upstream      ManifestUpstream  `json:"upstream"`
	Runtime       ManifestRuntime   `json:"runtime"`
	AppServer     ManifestAppServer `json:"appServer"`
	Patches       []ManifestPatch   `json:"patches"`
	BuildLock     ManifestBuildLock `json:"buildLock"`
}

type ManifestUpstream struct {
	URL    string `json:"url"`
	Tag    string `json:"tag"`
	Commit string `json:"commit"`
}

type ManifestPatch struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type ManifestRuntime struct {
	Binary          string `json:"binary"`
	ReportedVersion string `json:"reportedVersion"`
	SHA256          string `json:"sha256"`
	SizeBytes       int64  `json:"sizeBytes"`
	Target          string `json:"target"`
	Version         string `json:"version"`
}

type ManifestAppServer struct {
	ExperimentalAPI  bool   `json:"experimentalApi"`
	SchemaFileCount  int    `json:"schemaFileCount"`
	SchemaTreeSHA256 string `json:"schemaTreeSha256"`
	Transport        string `json:"transport"`
}

type ManifestBuildLock struct {
	FromVersion            string `json:"fromVersion"`
	NormalizedPackageCount int    `json:"normalizedPackageCount"`
	Policy                 string `json:"policy"`
	ResolvedLockSHA256     string `json:"resolvedLockSha256"`
	SchemaVersion          int    `json:"schemaVersion"`
	ToVersion              string `json:"toVersion"`
	UpstreamLockSHA256     string `json:"upstreamLockSha256"`
}

type artifactPolicy struct {
	runtimeSHA256    string
	runtimeSize      int64
	manifestSHA256   string
	schemaTreeSHA256 string
	patches          []ManifestPatch
}

var runtimeBaseline0Policy = artifactPolicy{
	runtimeSHA256:  ExpectedRuntimeSHA256,
	runtimeSize:    ExpectedRuntimeSize,
	manifestSHA256: ExpectedRuntimeManifestSHA256,
}

// Active after Owner termination of FEAT-137. The old four-patch constants
// remain historical test/source evidence, never the production artifact policy.
// Authority: yijie-contracts@4d3f967938dde1c86ca34003a0a5628717f96262
// docs/retirements/FEAT-137.json (SHA-256 67d7dfe8d539668a366ed744d92483d39597208259fe929d32d6d811c79ffbb8).
var retiredApprovalBaselinePolicy = artifactPolicy{
	runtimeSHA256:    "4efe16d2848680752cf9aacf4c17741ab2eeb7415894a66c2bb03652b00a322d",
	runtimeSize:      355676760,
	manifestSHA256:   "1cfa2e0a139b2213f4d29b1efeed71d4810110ac865f0bcbd931ff33b0062c1b",
	schemaTreeSHA256: "82ee9de771cf1d41bac16d87380f1121e7794107aa3aa526ad702d5d1bf7afe1",
	patches: []ManifestPatch{
		{Path: ExpectedRuntimePatch1Path, SHA256: ExpectedRuntimePatch1SHA256},
		{Path: ExpectedRuntimePatch2Path, SHA256: ExpectedRuntimePatch2SHA256},
	},
}

type ArtifactInfo struct {
	RuntimeVersion  string
	UpstreamTag     string
	UpstreamCommit  string
	Transport       string
	ExperimentalAPI bool
	BinarySHA256    string
	ManifestSHA256  string
}

func VerifyArtifact(ctx context.Context, binaryPath, manifestPath string, timeout time.Duration) (ArtifactInfo, error) {
	return verifyArtifactWithPolicy(ctx, binaryPath, manifestPath, timeout, retiredApprovalBaselinePolicy)
}

func verifyArtifactWithPolicy(
	ctx context.Context,
	binaryPath string,
	manifestPath string,
	timeout time.Duration,
	policy artifactPolicy,
) (ArtifactInfo, error) {
	if !filepath.IsAbs(binaryPath) {
		return ArtifactInfo{}, errors.New("runtime binary path must be absolute")
	}
	if !filepath.IsAbs(manifestPath) {
		return ArtifactInfo{}, errors.New("runtime manifest path must be absolute")
	}

	manifest, err := readManifest(manifestPath)
	if err != nil {
		return ArtifactInfo{}, err
	}
	manifestSHA256, err := fileSHA256(manifestPath)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("hash runtime manifest: %w", err)
	}
	if manifestSHA256 != policy.manifestSHA256 {
		return ArtifactInfo{}, errors.New("runtime manifest SHA-256 does not match pinned artifact")
	}
	if err := validateManifest(manifest, policy); err != nil {
		return ArtifactInfo{}, err
	}

	info, err := os.Stat(binaryPath)
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("stat runtime binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ArtifactInfo{}, errors.New("runtime binary is not a regular file")
	}
	if info.Mode().Perm()&0o111 == 0 {
		return ArtifactInfo{}, errors.New("runtime binary is not executable")
	}
	if filepath.Base(binaryPath) != manifest.Runtime.Binary {
		return ArtifactInfo{}, errors.New("runtime binary name does not match manifest")
	}
	if info.Size() != manifest.Runtime.SizeBytes {
		return ArtifactInfo{}, errors.New("runtime binary size does not match manifest")
	}

	digest, err := fileSHA256(binaryPath)
	if err != nil {
		return ArtifactInfo{}, err
	}
	if digest != manifest.Runtime.SHA256 {
		return ArtifactInfo{}, errors.New("runtime binary SHA-256 does not match manifest")
	}

	versionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := exec.CommandContext(versionCtx, binaryPath, "--version").CombinedOutput()
	if err != nil {
		return ArtifactInfo{}, fmt.Errorf("query runtime version: %w", err)
	}
	if strings.TrimSpace(string(output)) != ExpectedReportedVersion {
		return ArtifactInfo{}, errors.New("runtime reported version does not match baseline")
	}
	return ArtifactInfo{
		RuntimeVersion:  manifest.Runtime.Version,
		UpstreamTag:     manifest.Upstream.Tag,
		UpstreamCommit:  manifest.Upstream.Commit,
		Transport:       manifest.AppServer.Transport,
		ExperimentalAPI: manifest.AppServer.ExperimentalAPI,
		BinarySHA256:    digest,
		ManifestSHA256:  manifestSHA256,
	}, nil
}

func readManifest(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open runtime manifest: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Manifest{}, fmt.Errorf("stat runtime manifest: %w", err)
	}
	if info.Size() > 1<<20 {
		return Manifest{}, errors.New("runtime manifest exceeds 1 MiB")
	}

	var manifest Manifest
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode runtime manifest: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("runtime manifest contains trailing JSON")
		}
		return fmt.Errorf("decode runtime manifest trailing data: %w", err)
	}
	return nil
}

func validateManifest(manifest Manifest, policy artifactPolicy) error {
	schemaTree := policy.schemaTreeSHA256
	patches := policy.patches
	if patches == nil {
		// Existing internal fixtures cover the historical four-patch candidate.
		schemaTree = ExpectedSchemaTreeSHA256
		patches = []ManifestPatch{
			{Path: ExpectedRuntimePatch1Path, SHA256: ExpectedRuntimePatch1SHA256},
			{Path: ExpectedRuntimePatch2Path, SHA256: ExpectedRuntimePatch2SHA256},
			{Path: ExpectedRuntimePatch3Path, SHA256: ExpectedRuntimePatch3SHA256},
			{Path: ExpectedRuntimePatch4Path, SHA256: ExpectedRuntimePatch4SHA256},
		}
	}
	if len(manifest.Patches) != len(patches) {
		return errors.New("runtime patch count does not match reviewed overlay")
	}
	for index, expected := range patches {
		if manifest.Patches[index] != expected {
			return errors.New("runtime patch authority does not match reviewed overlay")
		}
	}
	switch {
	case manifest.SchemaVersion != ExpectedSchemaVersion:
		return errors.New("unsupported runtime manifest schema version")
	case manifest.Baseline != BaselineName:
		return errors.New("runtime manifest baseline does not match")
	case manifest.Upstream.URL != ExpectedUpstreamURL:
		return errors.New("runtime upstream URL does not match baseline")
	case manifest.Upstream.Tag != ExpectedUpstreamTag:
		return errors.New("runtime upstream tag does not match baseline")
	case manifest.Upstream.Commit != ExpectedUpstreamCommit:
		return errors.New("runtime upstream commit does not match baseline")
	case manifest.Runtime.Version != ExpectedRuntimeVersion:
		return errors.New("runtime version does not match baseline")
	case manifest.Runtime.ReportedVersion != ExpectedReportedVersion:
		return errors.New("runtime reported version metadata does not match baseline")
	case manifest.Runtime.Target != ExpectedTarget:
		return errors.New("runtime target does not match baseline")
	case manifest.Runtime.Binary != "codex":
		return errors.New("runtime binary name does not match baseline")
	case manifest.Runtime.SizeBytes != policy.runtimeSize:
		return errors.New("runtime binary size does not match pinned artifact")
	case manifest.Runtime.SHA256 != policy.runtimeSHA256:
		return errors.New("runtime binary SHA-256 does not match pinned artifact")
	case manifest.RustToolchain != ExpectedRustToolchain:
		return errors.New("runtime Rust toolchain does not match baseline")
	case manifest.AppServer.Transport != ExpectedTransport:
		return errors.New("runtime transport does not match baseline")
	case manifest.AppServer.ExperimentalAPI:
		return errors.New("experimental app-server API must remain disabled")
	case manifest.AppServer.SchemaFileCount != 267:
		return errors.New("app-server schema file count does not match baseline")
	case manifest.AppServer.SchemaTreeSHA256 != schemaTree:
		return errors.New("app-server schema tree SHA-256 does not match baseline")
	case manifest.BuildLock.SchemaVersion != 1:
		return errors.New("runtime build lock schema version does not match baseline")
	case manifest.BuildLock.FromVersion != "0.0.0" || manifest.BuildLock.ToVersion != ExpectedRuntimeVersion:
		return errors.New("runtime build lock version normalization does not match baseline")
	case manifest.BuildLock.NormalizedPackageCount != 132:
		return errors.New("runtime build lock package count does not match baseline")
	case manifest.BuildLock.Policy != "local-workspace-version-normalization-only":
		return errors.New("runtime build lock policy does not match baseline")
	case manifest.BuildLock.ResolvedLockSHA256 != ExpectedResolvedLockSHA256:
		return errors.New("resolved runtime build lock does not match baseline")
	case manifest.BuildLock.UpstreamLockSHA256 != ExpectedUpstreamLockSHA256:
		return errors.New("upstream runtime build lock does not match baseline")
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open runtime binary: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash runtime binary: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
