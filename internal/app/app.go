package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/36Dge/yijie-agent-host/internal/artifact"
	"github.com/36Dge/yijie-agent-host/internal/codex"
	agenthostcontract "github.com/36Dge/yijie-agent-host/internal/contracts"
	"github.com/36Dge/yijie-agent-host/internal/security"
	"github.com/36Dge/yijie-agent-host/internal/session"
	"github.com/google/uuid"
)

const (
	ServiceName                       = "yijie-agent-host"
	defaultSSEHeartbeatInterval       = 15 * time.Second
	feat137D4DeterministicProducerEnv = "YIJIE_FEAT137_D4_DETERMINISTIC_PRODUCER_ENABLED"
)

type Config struct {
	Environment                    string
	Port                           string
	HostHome                       string
	ParentPID                      int
	Runtime                        codex.Config
	RawReasoningV2Enabled          bool
	FEAT134StreamingEnabled        bool
	FEAT136CommandToolItemsEnabled bool
	FEAT137CommandApprovalEnabled  bool
	TitleV2Enabled                 bool
	CleanupV2Enabled               bool
	MultimodalV2Enabled            bool
	ArtifactV3Enabled              bool
	ArtifactSynthetic              bool
	ArtifactManifest               string
	ImageGenerationEnabled         bool
	InstanceNonce                  string
	FEAT126TestParentPID           int
	FEAT126TestRunID               string
	FEAT126TestProfile             string
	FEAT126ProjectDir              string
	Skills                         SkillFeatureConfig
}

type RuntimeStatusProvider interface {
	Snapshot() codex.Status
}

func LoadConfig() (Config, error) {
	// Owner permanently terminated FEAT-137 before acceptance. Reject old
	// opt-in flags before credentials, persistent state or Runtime are touched.
	for _, key := range []string{
		"YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED",
		"YIJIE_FEAT137_D4_DETERMINISTIC_PRODUCER_ENABLED",
		"YIJIE_FEAT137_DETERMINISTIC_APPROVAL_PRODUCER",
	} {
		if value := os.Getenv(key); value != "" && value != "false" && value != "0" {
			return Config{}, errors.New("FEAT-137 is permanently terminated; approval activation is unavailable")
		}
	}
	return loadConfigWithDirectoryAuthority(validateOwnerOnlyDirectoryAuthority)
}

type directoryAuthorityValidator func(value, authority string) (string, error)

func loadConfigWithDirectoryAuthority(validateDirectory directoryAuthorityValidator) (Config, error) {
	if validateDirectory == nil {
		return Config{}, errors.New("directory authority validator is required")
	}
	startupParentPID := os.Getppid()
	parentPID, err := parseAgentHostParentPID(os.Getenv("YIJIE_AGENT_HOST_PARENT_PID"), startupParentPID)
	if err != nil {
		return Config{}, err
	}
	runtimeConfig := codex.DefaultConfig()
	runtimeConfig.BinaryPath = os.Getenv("YIJIE_CODEX_BINARY")
	runtimeConfig.ManifestPath = os.Getenv("YIJIE_CODEX_MANIFEST")
	runtimeConfig.CodexHome = os.Getenv("YIJIE_CODEX_HOME")

	provider := os.Getenv("YIJIE_MODEL_PROVIDER")
	fakeProfile, err := loadFEAT126FakeResponsesProfile(validateDirectory, startupParentPID)
	if err != nil {
		return Config{}, err
	}
	if fakeProfile.Enabled {
		if provider != "" || os.Getenv("YIJIE_MINIMAX_API_KEY") != "" || os.Getenv("YIJIE_MINIMAX_API_KEY_FILE") != "" {
			return Config{}, errors.New("FEAT-126 fake Responses profile cannot be combined with MiniMax provider or key configuration")
		}
		runtimeConfig.FakeResponses = fakeProfile
	} else {
		miniMaxKey, keyErr := loadMiniMaxAPIKey()
		if keyErr != nil {
			return Config{}, keyErr
		}
		switch provider {
		case "":
			if miniMaxKey != "" {
				return Config{}, errors.New("YIJIE_MODEL_PROVIDER=minimax is required when a MiniMax key is configured")
			}
		case codex.MiniMaxProviderID:
			if miniMaxKey == "" {
				return Config{}, errors.New("MiniMax provider requires YIJIE_MINIMAX_API_KEY or YIJIE_MINIMAX_API_KEY_FILE")
			}
			runtimeConfig.MiniMax = codex.MiniMaxConfig{Enabled: true, APIKey: miniMaxKey}
		default:
			return Config{}, fmt.Errorf("unsupported YIJIE_MODEL_PROVIDER %q", provider)
		}
	}
	testParentPID := 0
	if fakeProfile.Enabled {
		testParentPID, _ = strconv.Atoi(os.Getenv("YIJIE_FEAT126_S10_PARENT_PID"))
		if parentPID == 0 {
			parentPID = testParentPID
		}
	}

	if runtimeConfig.StartupTimeout, err = durationEnv("YIJIE_CODEX_STARTUP_TIMEOUT", runtimeConfig.StartupTimeout); err != nil {
		return Config{}, err
	}
	if runtimeConfig.RequestTimeout, err = durationEnv("YIJIE_CODEX_REQUEST_TIMEOUT", runtimeConfig.RequestTimeout); err != nil {
		return Config{}, err
	}
	if runtimeConfig.ShutdownTimeout, err = durationEnv("YIJIE_CODEX_SHUTDOWN_TIMEOUT", runtimeConfig.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if runtimeConfig.MaxMessageBytes, err = intEnv("YIJIE_CODEX_MAX_MESSAGE_BYTES", runtimeConfig.MaxMessageBytes); err != nil {
		return Config{}, err
	}
	if runtimeConfig.WriteQueueDepth, err = intEnv("YIJIE_CODEX_WRITE_QUEUE_DEPTH", runtimeConfig.WriteQueueDepth); err != nil {
		return Config{}, err
	}
	if runtimeConfig.StderrTailBytes, err = intEnv("YIJIE_CODEX_STDERR_TAIL_BYTES", runtimeConfig.StderrTailBytes); err != nil {
		return Config{}, err
	}
	rawV2, err := boolEnv("YIJIE_AGENT_HOST_V2_RAW_REASONING_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	titleV2, err := boolEnv("YIJIE_AGENT_HOST_V2_TITLE_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	cleanupV2, err := boolEnv("YIJIE_AGENT_HOST_V2_CLEANUP_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	multimodalV2, err := boolEnv("YIJIE_AGENT_HOST_V2_MULTIMODAL_TURNS_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	artifactV3, err := boolEnv("YIJIE_AGENT_HOST_V3_ARTIFACTS_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	imageGeneration, err := exactBoolEnv("YIJIE_FEAT128_IMAGE_GENERATION_ENABLED")
	if err != nil {
		return Config{}, err
	}
	syntheticArtifact, artifactManifest, err := loadFEAT128SyntheticProfile()
	if err != nil {
		return Config{}, err
	}
	feat128S10Profile, err := loadFEAT128S10TestProfile()
	if err != nil {
		return Config{}, err
	}
	rawInstanceNonce := os.Getenv("YIJIE_AGENT_HOST_INSTANCE_NONCE")
	instanceNonce := strings.TrimSpace(rawInstanceNonce)
	if instanceNonce != "" {
		if !isCanonicalUUID(instanceNonce) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_INSTANCE_NONCE must be a canonical non-zero UUID")
		}
		if fakeProfile.Enabled && (rawInstanceNonce != instanceNonce || !isCanonicalRFC4122UUIDv4(instanceNonce)) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_INSTANCE_NONCE must be a canonical RFC4122 UUIDv4 for the FEAT-126 profile")
		}
	}

	hostHome := os.Getenv("YIJIE_AGENT_HOST_HOME")
	environment := env("YIJIE_ENV", "local")
	if fakeProfile.Enabled {
		if environment != "local" {
			return Config{}, errors.New("FEAT-126 fake Responses profile is local-only")
		}
		if !rawV2 || !cleanupV2 || titleV2 {
			return Config{}, errors.New("FEAT-126 fake Responses profile requires raw reasoning and cleanup enabled with title disabled")
		}
	}
	if (rawV2 || titleV2 || cleanupV2 || multimodalV2) && (environment != "local" || hostHome == "" || !filepath.IsAbs(hostHome)) {
		return Config{}, errors.New("Agent Host v2 draft capabilities require an absolute Host home in the local environment")
	}
	if artifactV3 && (environment != "local" || hostHome == "" || !filepath.IsAbs(hostHome)) {
		return Config{}, errors.New("Agent Host v3 Artifact capability requires an absolute Host home in the local environment")
	}
	if syntheticArtifact && !artifactV3 {
		return Config{}, errors.New("FEAT-128 synthetic profile requires the v3 Artifact capability")
	}
	if imageGeneration && (environment != "local" || !artifactV3 || !multimodalV2 || !runtimeConfig.MiniMax.Enabled ||
		runtimeConfig.FakeResponses.Enabled || syntheticArtifact) {
		return Config{}, errors.New("FEAT-128 image generation requires the exact local MiniMax and v3 Artifact conjunction")
	}
	runtimeConfig.DynamicToolsEnabled = imageGeneration
	if feat128S10Profile && (environment != "local" || !artifactV3 || !syntheticArtifact ||
		artifactManifest != session.SyntheticArtifactManifest || !runtimeConfig.FakeResponses.Enabled ||
		runtimeConfig.MiniMax.Enabled) {
		return Config{}, errors.New("FEAT-128 S10 test profile requires the exact keyless local fake and synthetic conjunction")
	}
	if syntheticArtifact && (runtimeConfig.MiniMax.Enabled || runtimeConfig.FakeResponses.Enabled) && !feat128S10Profile {
		return Config{}, errors.New("FEAT-128 synthetic profile cannot be combined with a model provider or another fake profile")
	}
	if titleV2 && !runtimeConfig.MiniMax.Enabled {
		return Config{}, errors.New("Agent Host v2 title generation requires the configured pinned model provider")
	}
	if titleV2 {
		return Config{}, errors.New("Agent Host v2 title generation remains disabled because the pinned Runtime cannot capability-disable tools")
	}
	if runtimeConfig.MiniMax.Enabled || runtimeConfig.FakeResponses.Enabled {
		if hostHome == "" || !filepath.IsAbs(hostHome) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_HOME must be absolute when MiniMax is enabled")
		}
		if runtimeConfig.CodexHome != "" && filepath.Clean(hostHome) == filepath.Clean(runtimeConfig.CodexHome) {
			return Config{}, errors.New("YIJIE_AGENT_HOST_HOME and YIJIE_CODEX_HOME must be separate directories")
		}
	}
	feat134Streaming, err := loadFEAT134StreamingProfile(environment, hostHome, runtimeConfig)
	if err != nil {
		return Config{}, err
	}
	feat136CommandToolItems, err := loadFEAT136CommandToolItemsProfile(environment, feat134Streaming)
	if err != nil {
		return Config{}, err
	}
	feat137CommandApproval, err := loadFEAT137CommandApprovalProfile(
		environment, hostHome, feat134Streaming, feat136CommandToolItems, runtimeConfig,
	)
	if err != nil {
		return Config{}, err
	}
	runtimeConfig.CommandApprovalEnabled = feat137CommandApproval
	// FEAT-152 uses the retained native Runtime, independently of retired v6.
	runtimeConfig.RuntimePermissionsEnabled = environment == "local" && os.Getenv("YIJIE_LOCAL_PROFILE") == "demo_fast" && os.Getenv("YIJIE_RUNTIME_PERMISSIONS_ENABLED") == "true" && runtimeConfig.MiniMax.Enabled
	if value := os.Getenv("YIJIE_PERMISSION_VERIFICATION_BASE_URL"); value != "" {
		// Fixed loopback meter only; the meter forwards to the normal MiniMax
		// endpoint and never changes model request or response bodies.
		if !runtimeConfig.RuntimePermissionsEnabled || value != "http://127.0.0.1:18083/v1" {
			return Config{}, errors.New("permission verification meter requires the exact local profile")
		}
		runtimeConfig.PermissionVerificationBaseURL = value
	}
	runtimeConfig.PermissionVerificationPolicy, err = loadPermissionVerificationPolicy(
		runtimeConfig.RuntimePermissionsEnabled,
		runtimeConfig.PermissionVerificationBaseURL,
		os.Getenv("YIJIE_PERMISSION_VERIFICATION_POLICY_FILE"),
	)
	if err != nil {
		return Config{}, err
	}
	feat137D4DeterministicProducer, err := loadFEAT137D4DeterministicProducerProfile(feat137CommandApproval)
	if err != nil {
		return Config{}, err
	}
	runtimeConfig.DeterministicApprovalProducerEnabled = feat137D4DeterministicProducer
	if feat134Streaming {
		runtimeConfig.ManagedReasoningProfile = codex.ManagedReasoningProfileHighRaw
	}
	feat126ProjectDirectory := ""
	if fakeProfile.Enabled {
		feat126ProjectDirectory, err = validateFEAT126ProjectAuthority(
			fakeProfile,
			hostHome,
			runtimeConfig.CodexHome,
			instanceNonce,
			validateDirectory,
		)
		if err != nil {
			return Config{}, err
		}
	}
	skillFeature, err := loadSkillFeatureConfig()
	if err != nil {
		return Config{}, err
	}
	if err := validateSkillHostAuthority(skillFeature, hostHome); err != nil {
		return Config{}, err
	}

	return Config{
		Environment:                    environment,
		Port:                           env("YIJIE_AGENT_HOST_PORT", "18080"),
		HostHome:                       hostHome,
		ParentPID:                      parentPID,
		Runtime:                        runtimeConfig,
		RawReasoningV2Enabled:          rawV2,
		FEAT134StreamingEnabled:        feat134Streaming,
		FEAT136CommandToolItemsEnabled: feat136CommandToolItems,
		FEAT137CommandApprovalEnabled:  feat137CommandApproval,
		TitleV2Enabled:                 titleV2,
		CleanupV2Enabled:               cleanupV2,
		MultimodalV2Enabled:            multimodalV2,
		ArtifactV3Enabled:              artifactV3,
		ArtifactSynthetic:              syntheticArtifact,
		ArtifactManifest:               artifactManifest,
		ImageGenerationEnabled:         imageGeneration,
		InstanceNonce:                  instanceNonce,
		FEAT126TestParentPID:           testParentPID,
		FEAT126TestRunID:               fakeProfile.RunID,
		FEAT126TestProfile:             feat126TestProfileName(fakeProfile),
		FEAT126ProjectDir:              feat126ProjectDirectory,
		Skills:                         skillFeature,
	}, nil
}

func loadFEAT134StreamingProfile(environment, hostHome string, runtimeConfig codex.Config) (bool, error) {
	enabled, err := exactBoolEnv("YIJIE_FEAT134_STREAMING_ENABLED")
	if err != nil || !enabled {
		return false, err
	}
	rawEnvironment, environmentSet := os.LookupEnv("YIJIE_ENV")
	localProfile, profileSet := os.LookupEnv("YIJIE_LOCAL_PROFILE")
	if !environmentSet || !profileSet || rawEnvironment != "local" || environment != "local" || localProfile != "demo_fast" {
		return false, errors.New("FEAT-134 streaming requires the exact explicit local demo_fast profile")
	}
	if !runtimeConfig.MiniMax.Enabled || runtimeConfig.FakeResponses.Enabled {
		return false, errors.New("FEAT-134 streaming requires the managed MiniMax provider")
	}
	if !canonicalAbsolutePath(hostHome) || !canonicalAbsolutePath(runtimeConfig.CodexHome) {
		return false, errors.New("FEAT-134 streaming requires separate canonical absolute Host and Runtime homes")
	}
	sameAuthority, authorityErr := samePhysicalAuthorityAllowMissing(hostHome, runtimeConfig.CodexHome)
	if authorityErr != nil {
		return false, errors.New("FEAT-134 streaming cannot verify separate Host and Runtime homes")
	}
	if sameAuthority {
		return false, errors.New("FEAT-134 streaming requires separate canonical absolute Host and Runtime homes")
	}
	if runtimeConfig.DynamicToolsEnabled {
		return false, errors.New("FEAT-134 streaming requires experimental Runtime tools to remain disabled")
	}
	return true, nil
}

func loadFEAT136CommandToolItemsProfile(environment string, feat134Streaming bool) (bool, error) {
	enabled, err := exactBoolEnv("YIJIE_FEAT136_COMMAND_TOOL_ITEMS_ENABLED")
	if err != nil || !enabled {
		return false, err
	}
	rawEnvironment, environmentSet := os.LookupEnv("YIJIE_ENV")
	localProfile, profileSet := os.LookupEnv("YIJIE_LOCAL_PROFILE")
	if !environmentSet || !profileSet || rawEnvironment != "local" || environment != "local" || localProfile != "demo_fast" {
		return false, errors.New("FEAT-136 Command and Tool items require the exact explicit local demo_fast profile")
	}
	if !feat134Streaming {
		return false, errors.New("FEAT-136 Command and Tool items require FEAT-134 streaming")
	}
	return true, nil
}

func loadFEAT137CommandApprovalProfile(
	environment, hostHome string,
	feat134Streaming, feat136CommandToolItems bool,
	runtimeConfig codex.Config,
) (bool, error) {
	enabled, err := exactBoolEnv("YIJIE_FEAT137_COMMAND_APPROVAL_ENABLED")
	if err != nil || !enabled {
		return false, err
	}
	rawEnvironment, environmentSet := os.LookupEnv("YIJIE_ENV")
	localProfile, profileSet := os.LookupEnv("YIJIE_LOCAL_PROFILE")
	if !environmentSet || !profileSet || rawEnvironment != "local" || environment != "local" || localProfile != "demo_fast" {
		return false, errors.New("FEAT-137 Command approval requires the exact explicit local demo_fast profile")
	}
	if !feat134Streaming || !feat136CommandToolItems {
		return false, errors.New("FEAT-137 Command approval requires FEAT-134 streaming and FEAT-136 Command items")
	}
	if !canonicalAbsolutePath(hostHome) || !runtimeConfig.MiniMax.Enabled || runtimeConfig.FakeResponses.Enabled ||
		runtimeConfig.DynamicToolsEnabled {
		return false, errors.New("FEAT-137 Command approval requires the stable read-only managed Runtime profile")
	}
	return true, nil
}

func loadFEAT137D4DeterministicProducerProfile(commandApprovalEnabled bool) (bool, error) {
	enabled, err := exactBoolEnv(feat137D4DeterministicProducerEnv)
	if err != nil || !enabled {
		return false, err
	}
	if !commandApprovalEnabled {
		return false, errors.New("FEAT-137 D4 deterministic producer requires the exact command approval profile")
	}
	return true, nil
}

type physicalAuthorityPath struct {
	path     string
	info     os.FileInfo
	missing  []string
	complete bool
}

func samePhysicalAuthorityAllowMissing(first, second string) (bool, error) {
	firstAuthority, err := resolvePhysicalAuthorityPath(first)
	if err != nil {
		return false, err
	}
	secondAuthority, err := resolvePhysicalAuthorityPath(second)
	if err != nil {
		return false, err
	}
	if firstAuthority.complete && secondAuthority.complete {
		return os.SameFile(firstAuthority.info, secondAuthority.info), nil
	}
	if os.SameFile(firstAuthority.info, secondAuthority.info) &&
		equalPathComponents(firstAuthority.missing, secondAuthority.missing) {
		return true, nil
	}
	return firstAuthority.path == secondAuthority.path, nil
}

func equalPathComponents(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func resolvePhysicalAuthorityPath(value string) (physicalAuthorityPath, error) {
	candidate := value
	missing := make([]string, 0, 2)
	for {
		info, err := os.Lstat(candidate)
		if err == nil {
			resolved, resolveErr := filepath.EvalSymlinks(candidate)
			if resolveErr != nil {
				return physicalAuthorityPath{}, resolveErr
			}
			resolvedInfo, statErr := os.Stat(resolved)
			if statErr != nil {
				return physicalAuthorityPath{}, statErr
			}
			orderedMissing := make([]string, len(missing))
			for index := len(missing) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, missing[index])
				orderedMissing[len(missing)-1-index] = missing[index]
			}
			return physicalAuthorityPath{
				path:     filepath.Clean(resolved),
				info:     resolvedInfo,
				missing:  orderedMissing,
				complete: len(missing) == 0 && info != nil,
			}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return physicalAuthorityPath{}, err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return physicalAuthorityPath{}, err
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

func loadFEAT128SyntheticProfile() (bool, string, error) {
	const (
		enabledKey  = "YIJIE_FEAT128_SYNTHETIC_ENABLED"
		manifestKey = "YIJIE_FEAT128_SYNTHETIC_MANIFEST"
	)
	enabled := os.Getenv(enabledKey)
	manifest := os.Getenv(manifestKey)
	if enabled == "" || enabled == "false" {
		if manifest != "" {
			return false, "", errors.New("FEAT-128 synthetic manifest requires the exact-true profile")
		}
		return false, "", nil
	}
	if enabled != "true" {
		return false, "", errors.New("YIJIE_FEAT128_SYNTHETIC_ENABLED must be exact true or false")
	}
	if manifest != session.SyntheticArtifactManifest {
		return false, "", errors.New("YIJIE_FEAT128_SYNTHETIC_MANIFEST must select feat128-artifact-v1")
	}
	return true, manifest, nil
}

func loadFEAT128S10TestProfile() (bool, error) {
	const key = "YIJIE_FEAT128_S10_TEST_PROFILE_ENABLED"
	switch os.Getenv(key) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("YIJIE_FEAT128_S10_TEST_PROFILE_ENABLED must be exact true or false")
	}
}

func feat126TestProfileName(profile codex.FakeResponsesConfig) string {
	if profile.Enabled {
		return "feat-126-s10-local-lab"
	}
	return ""
}

func loadFEAT126FakeResponsesProfile(validateDirectory directoryAuthorityValidator, startupParentPID int) (codex.FakeResponsesConfig, error) {
	const (
		masterKey          = "YIJIE_FEAT126_S10_TEST_PROFILE_ENABLED"
		runIDKey           = "YIJIE_FEAT126_S10_RUN_ID"
		baseURLKey         = "YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL"
		parentPIDKey       = "YIJIE_FEAT126_S10_PARENT_PID"
		hostLogDirKey      = "YIJIE_FEAT126_S10_HOST_LOG_DIR"
		processManifestKey = "YIJIE_FEAT126_S10_PROCESS_MANIFEST"
	)
	master := os.Getenv(masterKey)
	runID := os.Getenv(runIDKey)
	baseURL := os.Getenv(baseURLKey)
	parentPID := os.Getenv(parentPIDKey)
	hostLogDir := os.Getenv(hostLogDirKey)
	processManifest := os.Getenv(processManifestKey)
	if master != "true" {
		if master != "" && master != "false" {
			return codex.FakeResponsesConfig{}, fmt.Errorf("%s must be exact true or false", masterKey)
		}
		if runID != "" || baseURL != "" || parentPID != "" || hostLogDir != "" || processManifest != "" {
			return codex.FakeResponsesConfig{}, errors.New("FEAT-126 fake Responses settings require the exact-true test profile")
		}
		return codex.FakeResponsesConfig{}, nil
	}
	if !isCanonicalRFC4122UUIDv4(runID) {
		return codex.FakeResponsesConfig{}, errors.New("YIJIE_FEAT126_S10_RUN_ID must be a canonical RFC4122 UUIDv4")
	}
	if baseURL != codex.FEAT126FakeBaseURL {
		return codex.FakeResponsesConfig{}, errors.New("YIJIE_FEAT126_FAKE_RESPONSES_BASE_URL must use the fixed loopback endpoint")
	}
	parsedParentPID, err := strconv.Atoi(parentPID)
	if err != nil || parsedParentPID <= 0 || parsedParentPID != startupParentPID {
		return codex.FakeResponsesConfig{}, errors.New("YIJIE_FEAT126_S10_PARENT_PID must match the Host parent process")
	}
	cleanLogDir, err := validateDirectory(hostLogDir, "YIJIE_FEAT126_S10_HOST_LOG_DIR")
	if err != nil {
		return codex.FakeResponsesConfig{}, err
	}
	manifestParent, parentErr := filepath.EvalSymlinks(filepath.Dir(processManifest))
	if !filepath.IsAbs(processManifest) || parentErr != nil || filepath.Clean(manifestParent) != cleanLogDir || filepath.Base(processManifest) != "process.json" {
		return codex.FakeResponsesConfig{}, errors.New("YIJIE_FEAT126_S10_PROCESS_MANIFEST must be the run-scoped process.json")
	}
	if info, statErr := os.Lstat(processManifest); statErr == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
			return codex.FakeResponsesConfig{}, errors.New("FEAT-126 process manifest must be an owner-only regular file")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return codex.FakeResponsesConfig{}, errors.New("FEAT-126 process manifest cannot be inspected")
	}
	return codex.FakeResponsesConfig{
		Enabled: true, BaseURL: baseURL, RunID: runID, FixtureID: codex.FEAT126FakeFixtureID,
	}, nil
}

func parseAgentHostParentPID(value string, startupParentPID int) (int, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 1 || strconv.Itoa(parsed) != value || parsed != startupParentPID {
		return 0, errors.New("YIJIE_AGENT_HOST_PARENT_PID must exactly match the Host parent process")
	}
	return parsed, nil
}

func validateOwnerOnlyDirectoryAuthority(value, authority string) (string, error) {
	return validateOwnerOnlyDirectoryAuthorityForUID(value, authority, uint32(os.Geteuid()))
}

func validateOwnerOnlyDirectoryAuthorityForUID(value, authority string, expectedUID uint32) (string, error) {
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return "", fmt.Errorf("%s must be a canonical absolute path", authority)
	}
	info, err := os.Lstat(value)
	if err != nil {
		return "", fmt.Errorf("%s cannot be inspected", authority)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !hasExactOwnerDirectoryIdentity(info.Mode(), stat.Uid, expectedUID) {
		return "", fmt.Errorf("%s must be an owner-only non-symlink directory", authority)
	}
	resolved, err := filepath.EvalSymlinks(value)
	if err != nil || resolved != value {
		return "", fmt.Errorf("%s cannot be resolved", authority)
	}
	return resolved, nil
}

func hasExactOwnerDirectoryIdentity(mode os.FileMode, actualUID, expectedUID uint32) bool {
	return actualUID == expectedUID && mode.IsDir() && mode&os.ModeSymlink == 0 && mode.Perm() == 0o700
}

func validateFEAT126ProjectAuthority(
	profile codex.FakeResponsesConfig,
	hostHome, codexHome, instanceNonce string,
	validateDirectory directoryAuthorityValidator,
) (string, error) {
	if !profile.Enabled || !isCanonicalRFC4122UUIDv4(profile.RunID) || !isCanonicalRFC4122UUIDv4(instanceNonce) {
		return "", errors.New("FEAT-126 project authority requires the exact test profile and instance nonce")
	}
	logDirectory, err := validateDirectory(
		os.Getenv("YIJIE_FEAT126_S10_HOST_LOG_DIR"),
		"YIJIE_FEAT126_S10_HOST_LOG_DIR",
	)
	if err != nil {
		return "", err
	}
	hostEvidenceRoot := filepath.Dir(logDirectory)
	runRoot := filepath.Dir(hostEvidenceRoot)
	if filepath.Base(logDirectory) != instanceNonce || filepath.Base(hostEvidenceRoot) != "host" || filepath.Base(runRoot) != profile.RunID {
		return "", errors.New("FEAT-126 Host evidence directory is outside the exact run authority")
	}
	hostEvidenceRoot, err = validateDirectory(hostEvidenceRoot, "FEAT-126 Host evidence root")
	if err != nil {
		return "", err
	}
	runRoot, err = validateDirectory(runRoot, "FEAT-126 run root")
	if err != nil {
		return "", err
	}
	hostHome, err = validateDirectory(hostHome, "FEAT-126 Host home")
	if err != nil {
		return "", err
	}
	codexHome, err = validateDirectory(codexHome, "FEAT-126 Runtime home")
	if err != nil {
		return "", err
	}
	if hostEvidenceRoot != filepath.Join(runRoot, "host") || hostHome != filepath.Join(runRoot, "host-home") || codexHome != filepath.Join(runRoot, "codex-home") {
		return "", errors.New("FEAT-126 Host and Runtime homes are outside the exact run authority")
	}
	return validateDirectory(filepath.Join(runRoot, "project"), "FEAT-126 project directory")
}

func isCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value
}

func isCanonicalRFC4122UUIDv4(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed != uuid.Nil && parsed.String() == value &&
		parsed.Version() == uuid.Version(4) && parsed.Variant() == uuid.RFC4122
}

type SessionService interface {
	StartSession(context.Context, session.StartSessionInput) (session.Record, error)
	ResumeSession(context.Context, string, session.TraceContext) (session.Record, error)
	GetSession(string) (session.Record, error)
	StartTurn(context.Context, session.StartTurnInput) (codex.TurnInfo, error)
	InterruptTurn(context.Context, string, string, session.TraceContext) error
	SubscribeEvents(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
	SubscribeEventsV2(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
	GenerateTitle(context.Context, string, string, string) (session.TitleResult, error)
	CleanupSession(context.Context, string, string) (session.CleanupResult, error)
}

type multimodalSessionService interface {
	StartTurnV2(context.Context, session.StartTurnV2Input) (codex.TurnInfo, error)
}

type artifactSessionService interface {
	SubscribeEventsV3(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
	ReadArtifact(string, string, session.ArtifactResourceKind) (session.ArtifactResource, error)
	AcknowledgeArtifact(string, string, session.ArtifactAcknowledgement) (session.ArtifactReceipt, error)
}

type feat134SessionService interface {
	SubscribeEventsV4(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
}

type feat136SessionService interface {
	SubscribeEventsV5(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
}

type feat137SessionService interface {
	SubscribeEventsV6(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)
	PendingApprovals(string) (session.PendingApprovalSnapshot, error)
	DecideApproval(context.Context, string, string, session.ApprovalDecisionInput) (session.ApprovalDecisionResult, error)
}

func NewHandler(config Config, runtime RuntimeStatusProvider, sessions SessionService, apiToken string, options ...HandlerOption) http.Handler {
	handlerOptions := handlerOptions{}
	for _, option := range options {
		if option != nil {
			option(&handlerOptions)
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		setInstanceNonceHeader(w, config.InstanceNonce)
		writeJSON(w, http.StatusOK, agenthostcontract.HealthResponse{
			Service: agenthostcontract.HealthResponseServiceYijieAgentHost,
			Status:  agenthostcontract.HealthResponseStatusOk,
		})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		setInstanceNonceHeader(w, config.InstanceNonce)
		status := runtime.Snapshot()
		if !status.Ready {
			writeJSON(w, http.StatusServiceUnavailable, agenthostcontract.NotReadyResponse{
				Status:       agenthostcontract.NotReady,
				RuntimeState: agenthostcontract.RuntimeState(status.State),
			})
			return
		}
		writeJSON(w, http.StatusOK, agenthostcontract.ReadyResponse{
			Status:       agenthostcontract.ReadyResponseStatusReady,
			RuntimeState: agenthostcontract.ReadyResponseRuntimeStateReady,
		})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		runtimeStatus := runtime.Snapshot()
		serviceStatus := agenthostcontract.AgentHostStatusStatusDegraded
		if runtimeStatus.Ready {
			serviceStatus = agenthostcontract.AgentHostStatusStatusOk
		}
		writeJSON(w, http.StatusOK, agenthostcontract.AgentHostStatus{
			Service:     agenthostcontract.AgentHostStatusServiceYijieAgentHost,
			Status:      serviceStatus,
			Environment: config.Environment,
			RuntimeMode: agenthostcontract.ManagedStdio,
			Runtime:     runtimeStatusView(runtimeStatus),
		})
	})
	mux.HandleFunc("GET /v1/feat126/runtime-evidence", func(w http.ResponseWriter, request *http.Request) {
		if config.FEAT126TestRunID == "" || config.FEAT126TestProfile == "" ||
			request.Header.Get("X-Yijie-Feat126-Run-Id") != config.FEAT126TestRunID ||
			request.Header.Get("X-Yijie-Feat126-Nonce") != config.InstanceNonce {
			writeJSON(w, http.StatusNotFound, map[string]string{"code": "not_found"})
			return
		}
		provider, ok := runtime.(interface {
			RuntimeEvidence(string, string, string) (codex.RuntimeEvidence, error)
		})
		if !ok {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "evidence_unavailable"})
			return
		}
		evidence, err := provider.RuntimeEvidence(config.FEAT126TestRunID, config.InstanceNonce, config.FEAT126TestProfile)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"code": "evidence_unavailable"})
			return
		}
		writeJSON(w, http.StatusOK, evidence)
	})
	if sessions != nil {
		handler := &sessionHandler{
			service: sessions, apiToken: apiToken, heartbeatInterval: defaultSSEHeartbeatInterval,
		}
		registerRuntimePermissions(mux, handler, config.Runtime.RuntimePermissionsEnabled)
		mux.Handle("POST /v1/tasks/{task_id}/agent-sessions", handler.authorize(http.HandlerFunc(handler.startSession)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/resume", handler.authorize(http.HandlerFunc(handler.resumeSession)))
		mux.Handle("GET /v1/agent-sessions/{agent_session_id}", handler.authorize(http.HandlerFunc(handler.getSession)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/turns", handler.authorize(http.HandlerFunc(handler.startTurn)))
		mux.Handle("POST /v1/agent-sessions/{agent_session_id}/turns/{turn_id}/interrupt", handler.authorize(http.HandlerFunc(handler.interruptTurn)))
		mux.Handle("GET /v1/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.events)))
		if config.RawReasoningV2Enabled {
			mux.Handle("GET /v2/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.eventsV2)))
		}
		if config.TitleV2Enabled {
			mux.Handle("POST /v2/agent-sessions/{agent_session_id}/title-generations", handler.authorize(http.HandlerFunc(handler.generateTitleV2)))
		}
		if config.CleanupV2Enabled {
			mux.Handle("POST /v2/agent-sessions/{agent_session_id}/cleanup-operations", handler.authorize(http.HandlerFunc(handler.cleanupV2)))
		}
		if config.MultimodalV2Enabled {
			mux.Handle("POST /v2/agent-sessions/{agent_session_id}/turns", handler.authorize(http.HandlerFunc(handler.startTurnV2)))
		}
		if config.ArtifactV3Enabled {
			if _, ok := sessions.(artifactSessionService); ok {
				mux.Handle("GET /v3/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.eventsV3)))
				mux.Handle("GET /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/content", handler.authorize(http.HandlerFunc(handler.artifactContent)))
				mux.Handle("HEAD /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/content", handler.authorize(http.HandlerFunc(handler.artifactContent)))
				mux.Handle("GET /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/poster", handler.authorize(http.HandlerFunc(handler.artifactPoster)))
				mux.Handle("HEAD /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/poster", handler.authorize(http.HandlerFunc(handler.artifactPoster)))
				mux.Handle("POST /v3/agent-sessions/{agent_session_id}/artifacts/{artifact_id}/ack", handler.authorize(http.HandlerFunc(handler.artifactAck)))
			}
		}
		if config.FEAT134StreamingEnabled {
			if _, ok := sessions.(feat134SessionService); ok {
				mux.Handle("GET /v4/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.eventsV4)))
			}
		}
		if config.FEAT134StreamingEnabled && config.FEAT136CommandToolItemsEnabled {
			if _, ok := sessions.(feat136SessionService); ok {
				mux.Handle("GET /v5/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.eventsV5)))
			}
		}
		if config.Environment == "local" && config.FEAT134StreamingEnabled && config.FEAT136CommandToolItemsEnabled &&
			config.FEAT137CommandApprovalEnabled && config.Runtime.CommandApprovalEnabled {
			if _, ok := sessions.(feat137SessionService); ok {
				mux.Handle("GET /v6/agent-sessions/{agent_session_id}/events", handler.authorize(http.HandlerFunc(handler.eventsV6)))
				mux.Handle("GET /v6/agent-sessions/{agent_session_id}/approvals/pending", handler.authorize(http.HandlerFunc(handler.pendingApprovalsV6)))
				mux.Handle("POST /v6/agent-sessions/{agent_session_id}/approvals/{approval_request_id}/decision", handler.authorize(http.HandlerFunc(handler.decideApprovalV6)))
			}
		}
	}
	registerSkillRoutes(mux, config.Skills, handlerOptions.skillService, apiToken)
	return mux
}

func setInstanceNonceHeader(w http.ResponseWriter, nonce string) {
	if nonce != "" {
		w.Header().Set("X-Yijie-Host-Instance-Nonce", nonce)
	}
}

type sessionHandler struct {
	service           SessionService
	apiToken          string
	heartbeatInterval time.Duration
}

func (h *sessionHandler) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !security.TokenMatches(h.apiToken, r.Header.Get("Authorization")) {
			writeAPIError(w, http.StatusUnauthorized, agenthostcontract.ErrorResponseErrorCodeUnauthorized, "valid Agent Host bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *sessionHandler) startSession(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.StartSessionRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	record, err := h.service.StartSession(r.Context(), session.StartSessionInput{
		TaskID: r.PathValue("task_id"),
		Cwd:    request.Cwd,
		Trace:  traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeSessionResponse(w, http.StatusCreated, record)
}

func (h *sessionHandler) resumeSession(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.TraceRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	record, err := h.service.ResumeSession(
		r.Context(),
		r.PathValue("agent_session_id"),
		traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeSessionResponse(w, http.StatusOK, record)
}

func (h *sessionHandler) getSession(w http.ResponseWriter, r *http.Request) {
	record, err := h.service.GetSession(r.PathValue("agent_session_id"))
	if err != nil {
		writeSessionError(w, err)
		return
	}
	writeSessionResponse(w, http.StatusOK, record)
}

func (h *sessionHandler) startTurn(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.StartTurnRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	turn, err := h.service.StartTurn(r.Context(), session.StartTurnInput{
		AgentSessionID:  r.PathValue("agent_session_id"),
		Input:           request.Input,
		ReasoningEffort: reasoningEffort(request.ReasoningEffort),
		Trace:           traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	turnID, err := uuid.Parse(turn.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, http.StatusAccepted, agenthostcontract.StartTurnResponse{TurnId: turnID})
}

func (h *sessionHandler) startTurnV2(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(multimodalSessionService)
	if !ok {
		writeSessionError(w, session.ErrSessionNotUsable)
		return
	}
	var request agenthostcontract.StartTurnV2Request
	if err := decodeRequestWithLimit(w, r, &request, 16<<20); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	blocks, err := mapTurnV2ContentBlocks(request.ContentBlocks)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
		return
	}
	turn, err := service.StartTurnV2(r.Context(), session.StartTurnV2Input{
		AgentSessionID:  r.PathValue("agent_session_id"),
		OperationID:     request.OperationId.String(),
		ContentBlocks:   blocks,
		ReasoningEffort: reasoningEffortV2(request.ReasoningEffort),
		Trace:           traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	})
	if err != nil {
		writeSessionError(w, err)
		return
	}
	turnID, err := uuid.Parse(turn.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, http.StatusAccepted, agenthostcontract.StartTurnResponse{TurnId: turnID})
}

func (h *sessionHandler) interruptTurn(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.TraceRequest
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	if err := h.service.InterruptTurn(
		r.Context(),
		r.PathValue("agent_session_id"),
		r.PathValue("turn_id"),
		traceContext(request.TraceId, request.RequestId, request.TenantId, request.UserId),
	); err != nil {
		writeSessionError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *sessionHandler) events(w http.ResponseWriter, r *http.Request) {
	h.streamEvents(w, r, h.service.SubscribeEvents)
}

func (h *sessionHandler) eventsV2(w http.ResponseWriter, r *http.Request) {
	if values := r.URL.Query()["event_schema_version"]; len(values) != 1 || values[0] != "2" {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event_schema_version=2 is required")
		return
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "2")
	h.streamEvents(w, r, h.service.SubscribeEventsV2)
}

func (h *sessionHandler) eventsV3(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(artifactSessionService)
	if !ok {
		writeArtifactError(w, http.StatusNotFound, "artifact_not_found", "artifact capability is unavailable")
		return
	}
	query := r.URL.Query()
	for key := range query {
		if key != "event_schema_version" && key != "stream_id" && key != "after" {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event query is invalid")
			return
		}
	}
	if values := query["event_schema_version"]; len(values) != 1 || values[0] != "3" {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event_schema_version=3 is required")
		return
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "3")
	h.streamEvents(w, r, service.SubscribeEventsV3)
}

func (h *sessionHandler) eventsV4(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(feat134SessionService)
	if !ok {
		writeSessionError(w, session.ErrSessionNotUsable)
		return
	}
	query := r.URL.Query()
	for key := range query {
		if key != "event_schema_version" && key != "stream_id" && key != "after" {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event query is invalid")
			return
		}
	}
	if values := query["event_schema_version"]; len(values) != 1 || values[0] != "4" {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event_schema_version=4 is required")
		return
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "4")
	h.streamEvents(w, r, service.SubscribeEventsV4)
}

func (h *sessionHandler) eventsV5(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(feat136SessionService)
	if !ok {
		writeSessionError(w, session.ErrSessionNotUsable)
		return
	}
	query := r.URL.Query()
	for key := range query {
		if key != "event_schema_version" && key != "stream_id" && key != "after" {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event query is invalid")
			return
		}
	}
	if values := query["event_schema_version"]; len(values) != 1 || values[0] != "5" {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event_schema_version=5 is required")
		return
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "5")
	h.streamEvents(w, r, service.SubscribeEventsV5)
}

func (h *sessionHandler) eventsV6(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
		return
	}
	service, ok := h.service.(feat137SessionService)
	if !ok {
		writeSessionError(w, session.ErrSessionNotUsable)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event query is invalid")
		return
	}
	for key, values := range query {
		if key != "event_schema_version" && key != "stream_id" && key != "after" {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event query is invalid")
			return
		}
		if len(values) != 1 {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event query is invalid")
			return
		}
	}
	if values := query["event_schema_version"]; len(values) != 1 || values[0] != "6" {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event_schema_version=6 is required")
		return
	}
	sessionID, valid := normalizeUUIDV6(r.PathValue("agent_session_id"))
	if !valid {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
		return
	}
	lastEventIDs := r.Header.Values("Last-Event-ID")
	if len(lastEventIDs) > 1 {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
		return
	}
	lastEventID := ""
	if len(lastEventIDs) == 1 {
		streamID, sequence, found := strings.Cut(lastEventIDs[0], ":")
		normalized, valid := normalizeUUIDV6(streamID)
		if !found || !valid || sequence == "" {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
			return
		}
		lastEventID = normalized + ":" + sequence
	} else {
		if values, present := query["stream_id"]; present {
			normalized, valid := normalizeUUIDV6(values[0])
			if !valid {
				writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
				return
			}
			query.Set("stream_id", normalized)
		}
		if values, present := query["after"]; present && values[0] == "" {
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
			return
		}
	}
	normalizedRequest := r.Clone(r.Context())
	normalizedRequest.SetPathValue("agent_session_id", sessionID)
	normalizedRequest.URL.RawQuery = query.Encode()
	if lastEventID != "" {
		normalizedRequest.Header.Set("Last-Event-ID", lastEventID)
	}
	w.Header().Set("X-Yijie-Event-Schema-Version", "6")
	h.streamEvents(w, normalizedRequest, service.SubscribeEventsV6)
}

func (h *sessionHandler) pendingApprovalsV6(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		return
	}
	service, ok := h.service.(feat137SessionService)
	if !ok {
		writeApprovalError(w, session.ApprovalErrorUnavailable)
		return
	}
	if r.URL.RawQuery != "" {
		writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		return
	}
	sessionIDPath, valid := normalizeUUIDV6(r.PathValue("agent_session_id"))
	if !valid {
		writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		return
	}
	snapshot, err := service.PendingApprovals(sessionIDPath)
	if err != nil {
		writePendingApprovalServiceError(w, err)
		return
	}
	if !validPendingApprovalSnapshotV6(sessionIDPath, snapshot) {
		writeApprovalError(w, session.ApprovalErrorInternal)
		return
	}
	streamID, err := uuid.Parse(snapshot.StreamID)
	if err != nil {
		writeApprovalError(w, session.ApprovalErrorInternal)
		return
	}
	pending := make([]agenthostcontract.PendingApprovalV6, 0, len(snapshot.Pending))
	for _, item := range snapshot.Pending {
		approvalID, approvalErr := uuid.Parse(item.ApprovalRequestID)
		taskID, taskErr := uuid.Parse(item.TaskID)
		sessionID, sessionErr := uuid.Parse(item.AgentSessionID)
		threadID, threadErr := uuid.Parse(item.CodexThreadID)
		turnID, turnErr := uuid.Parse(item.TurnID)
		if approvalErr != nil || taskErr != nil || sessionErr != nil || threadErr != nil || turnErr != nil {
			writeApprovalError(w, session.ApprovalErrorInternal)
			return
		}
		pending = append(pending, agenthostcontract.PendingApprovalV6{
			ApprovalRequestId: approvalID, Revision: agenthostcontract.PendingApprovalV6Revision(item.Revision),
			TaskId: taskID, AgentSessionId: sessionID, CodexThreadId: threadID, TurnId: turnID,
			ItemId: item.ItemID, ActionId: agenthostcontract.PendingApprovalV6ActionId(item.ActionID),
			WorkspaceScope: agenthostcontract.PendingApprovalV6WorkspaceScope(item.WorkspaceScope),
			Decisions: agenthostcontract.ApprovalDecisionSetV6{
				Primary:   agenthostcontract.ApprovalDecisionSetV6PrimaryAcceptOnce,
				Secondary: agenthostcontract.ApprovalDecisionSetV6SecondaryCancelCurrentTurn,
			},
			RequestedAt: item.RequestedAt, ExpiresAt: item.ExpiresAt,
			TtlSeconds: agenthostcontract.PendingApprovalV6TtlSeconds(item.TTLSeconds),
		})
	}
	writeJSON(w, http.StatusOK, agenthostcontract.PendingApprovalSnapshotV6{
		SchemaVersion: agenthostcontract.PendingApprovalSnapshotV6SchemaVersion(snapshot.SchemaVersion),
		StreamId:      streamID, SnapshotAt: snapshot.SnapshotAt, Pending: pending,
	})
}

func (h *sessionHandler) decideApprovalV6(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(feat137SessionService)
	if !ok {
		writeApprovalError(w, session.ApprovalErrorUnavailable)
		return
	}
	if r.URL.RawQuery != "" {
		writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		return
	}
	sessionIDPath, validSessionID := normalizeUUIDV6(r.PathValue("agent_session_id"))
	approvalIDPath, validApprovalID := normalizeUUIDV6(r.PathValue("approval_request_id"))
	if !validSessionID || !validApprovalID {
		writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		return
	}
	request, versionMismatch, err := decodeApprovalDecisionV6Request(w, r)
	if err != nil {
		writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		return
	}
	if versionMismatch {
		writeApprovalError(w, session.ApprovalErrorVersionMismatch)
		return
	}
	input := session.ApprovalDecisionInput{
		SchemaVersion: int(request.SchemaVersion), DecisionID: request.DecisionId.String(),
		ExpectedStreamID: request.ExpectedStreamId.String(), ExpectedRevision: int64(request.ExpectedRevision),
		Decision: string(request.Decision),
	}
	result, err := service.DecideApproval(
		r.Context(), sessionIDPath, approvalIDPath, input,
	)
	if err != nil {
		writeApprovalServiceError(w, err)
		return
	}
	if !validApprovalDecisionResultV6(approvalIDPath, input, result) {
		writeApprovalError(w, session.ApprovalErrorInternal)
		return
	}
	approvalID, approvalErr := uuid.Parse(result.ApprovalRequestID)
	decisionID, decisionErr := uuid.Parse(result.DecisionID)
	streamID, streamErr := uuid.Parse(result.StreamID)
	if approvalErr != nil || decisionErr != nil || streamErr != nil {
		writeApprovalError(w, session.ApprovalErrorInternal)
		return
	}
	writeJSON(w, http.StatusOK, agenthostcontract.ApprovalDecisionV6Response{
		SchemaVersion:     agenthostcontract.ApprovalDecisionV6ResponseSchemaVersion(result.SchemaVersion),
		ApprovalRequestId: approvalID, DecisionId: decisionID, StreamId: streamID,
		Revision:   agenthostcontract.ApprovalDecisionV6ResponseRevision(result.Revision),
		Decision:   agenthostcontract.ApprovalDecisionNameV6(result.Decision),
		Outcome:    agenthostcontract.ApprovalDecisionV6ResponseOutcome(result.Outcome),
		ResolvedAt: result.ResolvedAt,
	})
}

func decodeApprovalDecisionV6Request(
	w http.ResponseWriter,
	r *http.Request,
) (agenthostcontract.ApprovalDecisionV6Request, bool, error) {
	contentTypes := r.Header.Values("Content-Type")
	if len(contentTypes) != 1 {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("exactly one Content-Type is required")
	}
	mediaType, _, err := mime.ParseMediaType(contentTypes[0])
	if err != nil || mediaType != "application/json" {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("Content-Type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision must be an object")
	}
	allowed := map[string]struct{}{
		"schema_version": {}, "decision_id": {}, "expected_stream_id": {},
		"expected_revision": {}, "decision": {},
	}
	fields := make(map[string]json.RawMessage, len(allowed))
	for decoder.More() {
		token, tokenErr := decoder.Token()
		name, ok := token.(string)
		if tokenErr != nil || !ok {
			return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision has an invalid field")
		}
		if _, known := allowed[name]; !known {
			return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision has an unknown field")
		}
		if _, duplicate := fields[name]; duplicate {
			return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision has a duplicate field")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return agenthostcontract.ApprovalDecisionV6Request{}, false, err
		}
		fields[name] = append(json.RawMessage(nil), raw...)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision object is not closed")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision has trailing content")
	}
	if len(fields) != len(allowed) {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision omitted a required field")
	}

	var schemaVersion int
	if json.Unmarshal(fields["schema_version"], &schemaVersion) != nil {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval schema version is invalid")
	}
	if schemaVersion != int(agenthostcontract.ApprovalDecisionV6RequestSchemaVersionN6) {
		return agenthostcontract.ApprovalDecisionV6Request{}, true, nil
	}
	var decisionIDText, streamIDText, decisionText string
	var expectedRevision int64
	if json.Unmarshal(fields["decision_id"], &decisionIDText) != nil ||
		json.Unmarshal(fields["expected_stream_id"], &streamIDText) != nil ||
		json.Unmarshal(fields["expected_revision"], &expectedRevision) != nil ||
		json.Unmarshal(fields["decision"], &decisionText) != nil {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision field is invalid")
	}
	decisionIDText, validDecisionID := normalizeUUIDV6(decisionIDText)
	streamIDText, validStreamID := normalizeUUIDV6(streamIDText)
	if !validDecisionID || !validStreamID {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision UUID is invalid")
	}
	decisionID, _ := uuid.Parse(decisionIDText)
	streamID, _ := uuid.Parse(streamIDText)
	request := agenthostcontract.ApprovalDecisionV6Request{
		SchemaVersion:    agenthostcontract.ApprovalDecisionV6RequestSchemaVersion(schemaVersion),
		DecisionId:       decisionID,
		ExpectedStreamId: streamID,
		ExpectedRevision: agenthostcontract.ApprovalDecisionV6RequestExpectedRevision(expectedRevision),
		Decision:         agenthostcontract.ApprovalDecisionNameV6(decisionText),
	}
	if !request.ExpectedRevision.Valid() || !request.Decision.Valid() {
		return agenthostcontract.ApprovalDecisionV6Request{}, false, errors.New("approval decision enum is invalid")
	}
	return request, false, nil
}

func normalizeUUIDV6(value string) (string, bool) {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed == uuid.Nil || len(value) != 36 || !strings.EqualFold(parsed.String(), value) {
		return "", false
	}
	return parsed.String(), true
}

func validPendingApprovalSnapshotV6(sessionID string, snapshot session.PendingApprovalSnapshot) bool {
	if snapshot.SchemaVersion != 6 || !isCanonicalUUID(snapshot.StreamID) || snapshot.SnapshotAt.IsZero() ||
		len(snapshot.Pending) > 1 {
		return false
	}
	for _, pending := range snapshot.Pending {
		if !isCanonicalUUID(pending.ApprovalRequestID) || !isCanonicalUUID(pending.TaskID) ||
			!isCanonicalUUID(pending.AgentSessionID) || pending.AgentSessionID != sessionID ||
			!isCanonicalUUID(pending.CodexThreadID) || !isCanonicalUUID(pending.TurnID) ||
			pending.Revision != 1 || pending.ItemID == "" || utf8.RuneCountInString(pending.ItemID) > 256 ||
			len(pending.ItemID) > 1024 || pending.ActionID != "git_repository_check" ||
			pending.WorkspaceScope != "current_workspace" || pending.TTLSeconds != 120 ||
			pending.RequestedAt.IsZero() || pending.ExpiresAt.Sub(pending.RequestedAt) != 120*time.Second ||
			snapshot.SnapshotAt.Before(pending.RequestedAt) || !snapshot.SnapshotAt.Before(pending.ExpiresAt) {
			return false
		}
	}
	return true
}

func validApprovalDecisionResultV6(
	approvalID string,
	input session.ApprovalDecisionInput,
	result session.ApprovalDecisionResult,
) bool {
	if result.SchemaVersion != 6 || result.ApprovalRequestID != approvalID || result.DecisionID != input.DecisionID ||
		result.StreamID != input.ExpectedStreamID || result.Revision != 2 || result.Decision != input.Decision ||
		!isCanonicalUUID(result.ApprovalRequestID) || !isCanonicalUUID(result.DecisionID) ||
		!isCanonicalUUID(result.StreamID) || result.RequestedAt.IsZero() || result.ExpiresAt.IsZero() ||
		result.ResolvedAt.IsZero() || result.ExpiresAt.Sub(result.RequestedAt) != 120*time.Second ||
		result.ResolvedAt.Before(result.RequestedAt) || !result.ResolvedAt.Before(result.ExpiresAt) {
		return false
	}
	return (result.Decision == "accept_once" && result.Outcome == "accepted_once") ||
		(result.Decision == "cancel_current_turn" && result.Outcome == "cancelled_current_turn")
}

func writeApprovalServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeApprovalError(w, "session_not_found")
		return
	}
	var approvalErr *session.ApprovalError
	if errors.As(err, &approvalErr) {
		writeApprovalError(w, approvalErr.Code)
		return
	}
	writeApprovalError(w, session.ApprovalErrorInternal)
}

func writePendingApprovalServiceError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrNotFound) {
		writeApprovalError(w, "session_not_found")
		return
	}
	var approvalErr *session.ApprovalError
	if errors.As(err, &approvalErr) {
		switch approvalErr.Code {
		case session.ApprovalErrorInvalidRequest:
			writeApprovalError(w, session.ApprovalErrorInvalidRequest)
		case session.ApprovalErrorInternal:
			writeApprovalError(w, session.ApprovalErrorInternal)
		default:
			// The frozen pending-snapshot surface has no 503 response. Any
			// unavailable internal authority therefore remains a closed 500
			// instead of widening the immutable v6 HTTP contract.
			writeApprovalError(w, session.ApprovalErrorInternal)
		}
		return
	}
	writeApprovalError(w, session.ApprovalErrorInternal)
}

func writeApprovalError(w http.ResponseWriter, code string) {
	status := http.StatusInternalServerError
	message := "approval processing failed"
	switch code {
	case session.ApprovalErrorInvalidRequest:
		status, message = http.StatusBadRequest, "approval request is invalid"
	case session.ApprovalErrorVersionMismatch:
		status, message = http.StatusBadRequest, "approval schema version does not match"
	case "session_not_found":
		status, message = http.StatusNotFound, "agent session was not found"
	case session.ApprovalErrorNotFound:
		status, message = http.StatusNotFound, "approval request was not found"
	case session.ApprovalErrorStale:
		status, message = http.StatusConflict, "approval request is stale"
	case session.ApprovalErrorExpired:
		status, message = http.StatusConflict, "approval request expired"
	case session.ApprovalErrorAlreadyResolved:
		status, message = http.StatusConflict, "approval request was already resolved"
	case session.ApprovalErrorDecisionConflict:
		status, message = http.StatusConflict, "approval decision conflicts with the existing decision"
	case session.ApprovalErrorUnavailable:
		status, message = http.StatusServiceUnavailable, "approval authority is unavailable"
	case session.ApprovalErrorInternal:
		status, message = http.StatusInternalServerError, "approval processing failed"
	default:
		code = session.ApprovalErrorInternal
	}
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (h *sessionHandler) artifactContent(w http.ResponseWriter, r *http.Request) {
	h.writeArtifactResource(w, r, session.ArtifactContent)
}

func (h *sessionHandler) artifactPoster(w http.ResponseWriter, r *http.Request) {
	h.writeArtifactResource(w, r, session.ArtifactPoster)
}

func (h *sessionHandler) writeArtifactResource(w http.ResponseWriter, r *http.Request, kind session.ArtifactResourceKind) {
	service, ok := h.service.(artifactSessionService)
	if !ok {
		writeArtifactError(w, http.StatusNotFound, "artifact_not_found", "artifact was not found")
		return
	}
	resource, err := service.ReadArtifact(r.PathValue("agent_session_id"), r.PathValue("artifact_id"), kind)
	if err != nil {
		writeArtifactServiceError(w, err)
		return
	}
	start, end, partial, rangeErr := artifactRange(r.Header.Get("Range"), int64(len(resource.Bytes)), r.Method == http.MethodHead)
	if rangeErr != nil {
		if errors.Is(rangeErr, errArtifactRangeUnsatisfiable) {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(resource.Bytes)))
			writeArtifactError(w, http.StatusRequestedRangeNotSatisfiable, "artifact_range_not_satisfiable", "artifact byte range is not satisfiable")
			return
		}
		writeArtifactError(w, http.StatusBadRequest, "invalid_range", "artifact byte range is invalid")
		return
	}
	body := resource.Bytes[start : end+1]
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", resource.MediaType)
	w.Header().Set("Content-Length", strconv.FormatInt(int64(len(body)), 10))
	w.Header().Set("ETag", `"`+resource.SHA256+`"`)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": resource.DisplayName})
	if disposition == "" {
		writeArtifactError(w, http.StatusInternalServerError, "artifact_resource_unavailable", "artifact resource is unavailable")
		return
	}
	w.Header().Set("Content-Disposition", disposition)
	status := http.StatusOK
	if partial {
		status = http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(resource.Bytes)))
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}

func (h *sessionHandler) artifactAck(w http.ResponseWriter, r *http.Request) {
	service, ok := h.service.(artifactSessionService)
	if !ok {
		writeArtifactError(w, http.StatusNotFound, "artifact_not_found", "artifact was not found")
		return
	}
	var request agenthostcontract.ArtifactAcknowledgementV3Request
	if err := decodeRequest(w, r, &request); err != nil {
		writeArtifactError(w, http.StatusBadRequest, "invalid_request", "acknowledgement request is invalid")
		return
	}
	receipt, err := service.AcknowledgeArtifact(
		r.PathValue("agent_session_id"), r.PathValue("artifact_id"), session.ArtifactAcknowledgement{
			AckID: request.AckId.String(), SizeBytes: request.SizeBytes, SHA256: request.Sha256,
			LocalCommittedAt: request.LocalCommittedAt,
		},
	)
	if err != nil {
		writeArtifactServiceError(w, err)
		return
	}
	artifactID, artifactErr := uuid.Parse(receipt.ArtifactID)
	ackID, ackErr := uuid.Parse(receipt.AckID)
	if artifactErr != nil || ackErr != nil {
		writeArtifactError(w, http.StatusInternalServerError, "internal_error", "artifact acknowledgement failed")
		return
	}
	writeJSON(w, http.StatusOK, agenthostcontract.ArtifactAcknowledgementV3Response{
		ArtifactId: artifactID, AckId: ackID, Status: agenthostcontract.Acknowledged,
		CleanupStatus:  agenthostcontract.ArtifactAcknowledgementV3ResponseCleanupStatus(receipt.CleanupStatus),
		AcknowledgedAt: receipt.AcknowledgedAt,
	})
}

var errArtifactRangeUnsatisfiable = errors.New("artifact range is not satisfiable")

func artifactRange(value string, size int64, head bool) (int64, int64, bool, error) {
	if size < 1 {
		return 0, 0, false, errArtifactRangeUnsatisfiable
	}
	if value == "" {
		return 0, size - 1, false, nil
	}
	if head || !strings.HasPrefix(value, "bytes=") || strings.Contains(value, ",") {
		return 0, 0, false, errors.New("invalid artifact range")
	}
	raw := strings.TrimPrefix(value, "bytes=")
	parts := strings.Split(raw, "-")
	if len(parts) != 2 || (parts[0] == "" && parts[1] == "") {
		return 0, 0, false, errors.New("invalid artifact range")
	}
	if parts[0] == "" {
		suffix, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || suffix <= 0 {
			return 0, 0, false, errors.New("invalid artifact range")
		}
		if suffix > size {
			suffix = size
		}
		return size - suffix, size - 1, true, nil
	}
	start, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false, errors.New("invalid artifact range")
	}
	end := size - 1
	if parts[1] != "" {
		end, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil || end < 0 || end < start {
			return 0, 0, false, errors.New("invalid artifact range")
		}
	}
	if start >= size {
		return 0, 0, false, errArtifactRangeUnsatisfiable
	}
	if end >= size {
		end = size - 1
	}
	return start, end, true, nil
}

func writeArtifactServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, artifact.ErrInvalid):
		writeArtifactError(w, http.StatusBadRequest, "invalid_request", "artifact request is invalid")
	case errors.Is(err, artifact.ErrNotFound):
		writeArtifactError(w, http.StatusNotFound, "artifact_not_found", "artifact was not found")
	case errors.Is(err, artifact.ErrExpired):
		writeArtifactError(w, http.StatusGone, "artifact_expired", "artifact staging has expired")
	case errors.Is(err, artifact.ErrNotReady):
		writeArtifactError(w, http.StatusConflict, "artifact_not_ready", "artifact is not ready")
	case errors.Is(err, artifact.ErrManifestMismatch):
		writeArtifactError(w, http.StatusConflict, "artifact_manifest_mismatch", "artifact acknowledgement does not match")
	case errors.Is(err, artifact.ErrAckConflict):
		writeArtifactError(w, http.StatusConflict, "artifact_ack_conflict", "artifact acknowledgement conflicts")
	default:
		writeArtifactError(w, http.StatusInternalServerError, "artifact_resource_unavailable", "artifact resource is unavailable")
	}
}

func writeArtifactError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

type eventSubscriber func(string, string, uint64) (string, []session.Event, <-chan session.Event, func(), error)

func (h *sessionHandler) streamEvents(w http.ResponseWriter, r *http.Request, subscribe eventSubscriber) {
	streamID, after, err := eventCursor(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
		return
	}
	actualStreamID, replay, updates, cancel, err := subscribe(
		r.PathValue("agent_session_id"), streamID, after,
	)
	if err != nil {
		writeSessionError(w, err)
		return
	}
	defer cancel()
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeStreamingUnsupported, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Yijie-Event-Stream-ID", actualStreamID)
	w.WriteHeader(http.StatusOK)
	for _, event := range replay {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
	}
	flusher.Flush()
	heartbeatInterval := h.heartbeatInterval
	if heartbeatInterval <= 0 {
		heartbeatInterval = defaultSSEHeartbeatInterval
	}
	heartbeat := time.NewTimer(heartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case event, open := <-updates:
			if !open {
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
			resetTimer(heartbeat, heartbeatInterval)
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
			heartbeat.Reset(heartbeatInterval)
		case <-r.Context().Done():
			return
		}
	}
}

func (h *sessionHandler) generateTitleV2(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.GenerateTitleV2Request
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	result, err := h.service.GenerateTitle(r.Context(), r.PathValue("agent_session_id"), request.OperationId.String(), request.Input)
	if err != nil {
		switch {
		case errors.Is(err, session.ErrInvalidArgument):
			writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
		case errors.Is(err, session.ErrNotFound):
			writeAPIError(w, http.StatusNotFound, agenthostcontract.ErrorResponseErrorCodeSessionNotFound, "agent session was not found")
		case errors.Is(err, session.ErrTitleOperationConflict):
			writeNestedError(w, http.StatusConflict, "title_operation_conflict", "title operation conflicts with an existing input")
		case errors.Is(err, session.ErrTitleOutputInvalid):
			writeNestedError(w, http.StatusUnprocessableEntity, "title_output_invalid", "title output is invalid")
		default:
			writeNestedError(w, http.StatusServiceUnavailable, "title_generation_unavailable", "title generation is unavailable")
		}
		return
	}
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, http.StatusOK, agenthostcontract.GenerateTitleV2Response{OperationId: operationID, Title: result.Title})
}

func (h *sessionHandler) cleanupV2(w http.ResponseWriter, r *http.Request) {
	var request agenthostcontract.CleanupAgentSessionV2Request
	if err := decodeRequest(w, r, &request); err != nil {
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request body is invalid")
		return
	}
	result, err := h.service.CleanupSession(r.Context(), r.PathValue("agent_session_id"), request.OperationId.String())
	if err != nil {
		if errors.Is(err, session.ErrCleanupConflict) {
			writeNestedError(w, http.StatusConflict, "cleanup_operation_conflict", "cleanup operation conflicts with another session")
			return
		}
		writeSessionError(w, err)
		return
	}
	operationID, err := uuid.Parse(result.OperationID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	if result.Outcome == "complete" {
		writeJSON(w, http.StatusOK, agenthostcontract.CleanupAgentSessionV2CompletedResponse{
			OperationId: operationID, Outcome: agenthostcontract.CleanupAgentSessionV2CompletedResponseOutcomeComplete,
			Surfaces: agenthostcontract.CleanupCompletedSurfaces{
				RuntimeThreadTree: agenthostcontract.CleanupCompletedSurfacesRuntimeThreadTreeComplete,
				HostMapping:       agenthostcontract.CleanupCompletedSurfacesHostMappingComplete,
				HostReplay:        agenthostcontract.CleanupCompletedSurfacesHostReplayComplete,
			},
		})
		return
	}
	writeJSON(w, http.StatusConflict, agenthostcontract.CleanupAgentSessionV2IncompleteResponse{
		OperationId: operationID, Outcome: agenthostcontract.CleanupAgentSessionV2IncompleteResponseOutcomeIncomplete,
		Surfaces: agenthostcontract.CleanupIncompleteSurfaces{
			RuntimeThreadTree: agenthostcontract.CleanupSurfaceStatus(result.RuntimeThreadTree),
			HostMapping:       agenthostcontract.CleanupSurfaceStatus(result.HostMapping),
			HostReplay:        agenthostcontract.CleanupSurfaceStatus(result.HostReplay),
		},
		Error: agenthostcontract.CleanupIncompleteError{
			Code:       agenthostcontract.CleanupIncomplete,
			ReasonCode: agenthostcontract.CleanupIncompleteErrorReasonCode(result.ReasonCode),
			Message:    "agent session cleanup is incomplete",
		},
	})
}

func resetTimer(timer *time.Timer, interval time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(interval)
}

func traceContext(traceID, requestID, tenantID, userID *string) session.TraceContext {
	return session.TraceContext{
		TraceID: stringValue(traceID), RequestID: stringValue(requestID),
		TenantID: stringValue(tenantID), UserID: stringValue(userID),
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func reasoningEffort(value *agenthostcontract.StartTurnRequestReasoningEffort) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func reasoningEffortV2(value *agenthostcontract.StartTurnV2RequestReasoningEffort) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func mapTurnV2ContentBlocks(blocks []agenthostcontract.StartTurnV2ContentBlock) ([]session.TurnContentBlock, error) {
	mapped := make([]session.TurnContentBlock, 0, len(blocks))
	for _, block := range blocks {
		raw, err := block.MarshalJSON()
		if err != nil {
			return nil, err
		}
		discriminator, err := block.Discriminator()
		if err != nil {
			return nil, err
		}
		switch discriminator {
		case session.ContentBlockText:
			var value agenthostcontract.StartTurnV2TextBlock
			if err := strictDecodeJSON(raw, &value); err != nil || string(value.Type) != discriminator {
				return nil, errors.New("invalid text content block")
			}
			mapped = append(mapped, session.TurnContentBlock{Type: discriminator, Text: value.Text})
		case session.ContentBlockImage:
			var value agenthostcontract.StartTurnV2ImageBlock
			if err := strictDecodeJSON(raw, &value); err != nil || string(value.Type) != discriminator {
				return nil, errors.New("invalid image content block")
			}
			mapped = append(mapped, session.TurnContentBlock{Type: discriminator, Image: &session.TurnImageBlock{
				AttachmentID: value.AttachmentId.String(),
				MediaType:    string(value.MediaType),
				SizeBytes:    value.SizeBytes,
				SHA256:       value.Sha256,
				DataURL:      value.DataUrl,
			}})
		case session.ContentBlockFile:
			var value agenthostcontract.StartTurnV2FileBlock
			if err := strictDecodeJSON(raw, &value); err != nil || string(value.Type) != discriminator {
				return nil, errors.New("invalid file content block")
			}
			mapped = append(mapped, session.TurnContentBlock{Type: discriminator, File: &session.TurnFileBlock{
				AttachmentID:  value.AttachmentId.String(),
				Name:          value.Name,
				MediaType:     string(value.MediaType),
				SizeBytes:     value.SizeBytes,
				SHA256:        value.Sha256,
				ContextChunks: append([]string(nil), value.ContextChunks...),
			}})
		default:
			return nil, errors.New("unsupported content block discriminator")
		}
	}
	return mapped, nil
}

func strictDecodeJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON value has trailing content")
	}
	return nil
}

func decodeRequest(w http.ResponseWriter, r *http.Request, destination any) error {
	return decodeRequestWithLimit(w, r, destination, 1<<20)
}

func decodeRequestWithLimit(w http.ResponseWriter, r *http.Request, destination any, maxBytes int64) error {
	if contentType := r.Header.Get("Content-Type"); contentType != "" && !strings.HasPrefix(contentType, "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	if maxBytes < 1 {
		return errors.New("request body limit is invalid")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func eventCursor(r *http.Request) (string, uint64, error) {
	streamID := r.URL.Query().Get("stream_id")
	afterText := r.URL.Query().Get("after")
	fromLastEventID := false
	if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
		parts := strings.Split(lastEventID, ":")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", 0, errors.New("invalid Last-Event-ID")
		}
		if parts[1][0] == '0' {
			return "", 0, errors.New("invalid Last-Event-ID sequence")
		}
		streamID = parts[0]
		afterText = parts[1]
		fromLastEventID = true
	}
	if afterText == "" {
		return streamID, 0, nil
	}
	after, err := strconv.ParseUint(afterText, 10, 64)
	if err != nil || (fromLastEventID && after == 0) || (after > 0 && streamID == "") {
		return "", 0, errors.New("invalid event cursor")
	}
	return streamID, after, nil
}

func writeSSEEvent(w io.Writer, event session.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if bytes.ContainsAny(data, "\r\n") {
		return errors.New("encoded SSE event contains a line break")
	}
	_, err = fmt.Fprintf(w, "id: %s:%d\nevent: %s\ndata: %s\n\n", event.StreamID, event.Sequence, event.EventType, data)
	return err
}

func writeSessionResponse(w http.ResponseWriter, status int, record session.Record) {
	response, err := sessionResponse(record)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
		return
	}
	writeJSON(w, status, response)
}

func sessionResponse(record session.Record) (agenthostcontract.SessionResponse, error) {
	taskID, err := uuid.Parse(record.TaskID)
	if err != nil {
		return agenthostcontract.SessionResponse{}, fmt.Errorf("invalid persisted task id: %w", err)
	}
	agentSessionID, err := uuid.Parse(record.AgentSessionID)
	if err != nil {
		return agenthostcontract.SessionResponse{}, fmt.Errorf("invalid persisted agent session id: %w", err)
	}
	return agenthostcontract.SessionResponse{Session: agenthostcontract.AgentSession{
		TaskId:         taskID,
		AgentSessionId: agentSessionID,
		CodexThreadId:  record.CodexThreadID,
		ActiveTurnId:   record.ActiveTurnID,
		State:          agenthostcontract.AgentSessionState(record.State),
		Cwd:            record.Cwd,
		Model:          agenthostcontract.AgentSessionModel(record.Model),
		ModelProvider:  agenthostcontract.AgentSessionModelProvider(record.ModelProvider),
		FailureCode:    agenthostcontract.AgentSessionFailureCode(record.FailureCode),
		CreatedAt:      record.CreatedAt,
		UpdatedAt:      record.UpdatedAt,
	}}, nil
}

func runtimeStatusView(status codex.Status) agenthostcontract.RuntimeStatus {
	view := agenthostcontract.RuntimeStatus{
		State:           agenthostcontract.RuntimeState(status.State),
		Ready:           status.Ready,
		Transport:       agenthostcontract.RuntimeStatusTransport(status.Transport),
		ExperimentalApi: agenthostcontract.RuntimeStatusExperimentalApi(status.ExperimentalAPI),
	}
	if status.RuntimeVersion != "" {
		value := agenthostcontract.RuntimeStatusRuntimeVersion(status.RuntimeVersion)
		view.RuntimeVersion = &value
	}
	if status.UpstreamTag != "" {
		value := agenthostcontract.RuntimeStatusUpstreamTag(status.UpstreamTag)
		view.UpstreamTag = &value
	}
	if status.UpstreamCommit != "" {
		value := agenthostcontract.RuntimeStatusUpstreamCommit(status.UpstreamCommit)
		view.UpstreamCommit = &value
	}
	if status.FailureCode != "" {
		value := agenthostcontract.RuntimeStatusFailureCode(status.FailureCode)
		if !value.Valid() {
			// Internal lifecycle details are logged by the Runtime adapter. Never
			// project an unknown string outside the frozen closed status enum.
			value = agenthostcontract.ProtocolFailure
		}
		view.FailureCode = &value
	}
	if status.ModelProvider != "" {
		value := agenthostcontract.RuntimeStatusModelProvider(status.ModelProvider)
		view.ModelProvider = &value
	}
	if status.Model != "" {
		value := agenthostcontract.RuntimeStatusModel(status.Model)
		view.Model = &value
	}
	return view
}

func writeSessionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		writeAPIError(w, http.StatusNotFound, agenthostcontract.ErrorResponseErrorCodeSessionNotFound, "agent session was not found")
	case errors.Is(err, session.ErrTaskExists):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeTaskSessionExists, "task already has an agent session")
	case errors.Is(err, session.ErrTurnActive):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeTurnActive, "agent session already has an active turn")
	case errors.Is(err, session.ErrTurnNotActive):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeTurnNotActive, "turn is not active for this agent session")
	case errors.Is(err, session.ErrTurnOperationConflict):
		writeNestedError(w, http.StatusConflict, "turn_operation_conflict", "operation_id was already used with different turn input")
	case errors.Is(err, session.ErrSessionNotUsable), errors.Is(err, session.ErrTurnOperationPending):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeSessionNotUsable, "agent session cannot perform this operation")
	case errors.Is(err, session.ErrStreamChanged):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeEventStreamChanged, "event stream changed after Host restart")
	case errors.Is(err, session.ErrReplayUnavailable):
		writeAPIError(w, http.StatusConflict, agenthostcontract.ErrorResponseErrorCodeEventReplayUnavailable, "requested events are no longer available")
	case errors.Is(err, session.ErrInvalidSequence):
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidEventCursor, "event cursor is invalid")
	case errors.Is(err, session.ErrInvalidArgument):
		writeAPIError(w, http.StatusBadRequest, agenthostcontract.ErrorResponseErrorCodeInvalidRequest, "request parameters are invalid")
	case errors.Is(err, session.ErrRuntimeRequest):
		writeAPIError(w, http.StatusBadGateway, agenthostcontract.ErrorResponseErrorCodeRuntimeRequestFailed, "Codex Runtime request failed")
	default:
		writeAPIError(w, http.StatusInternalServerError, agenthostcontract.ErrorResponseErrorCodeInternalError, "Agent Host operation failed")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code agenthostcontract.ErrorResponseErrorCode, message string) {
	var response agenthostcontract.ErrorResponse
	response.Error.Code = code
	response.Error.Message = message
	writeJSON(w, status, response)
}

func writeNestedError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func loadMiniMaxAPIKey() (string, error) {
	direct := os.Getenv("YIJIE_MINIMAX_API_KEY")
	filePath := os.Getenv("YIJIE_MINIMAX_API_KEY_FILE")
	if direct != "" && filePath != "" {
		return "", errors.New("configure only one MiniMax API key source")
	}
	if direct != "" {
		if strings.TrimSpace(direct) != direct || strings.ContainsAny(direct, "\r\n\x00") {
			return "", errors.New("YIJIE_MINIMAX_API_KEY is invalid")
		}
		return direct, nil
	}
	if filePath == "" {
		return "", nil
	}
	if !filepath.IsAbs(filePath) {
		return "", errors.New("YIJIE_MINIMAX_API_KEY_FILE must be absolute")
	}
	info, err := os.Lstat(filePath)
	if err != nil {
		return "", fmt.Errorf("stat MiniMax API key file: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("MiniMax API key file must be regular and accessible only by its owner")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read MiniMax API key file: %w", err)
	}
	if len(content) > 16<<10 {
		return "", errors.New("MiniMax API key file exceeds 16 KiB")
	}
	key := strings.TrimSpace(string(content))
	if key == "" || strings.ContainsAny(key, "\r\n\x00") {
		return "", errors.New("MiniMax API key file is invalid")
	}
	return key, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		return
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func intEnv(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("parse %s: %w", key, err)
	}
	return parsed, nil
}

func exactBoolEnv(key string) (bool, error) {
	switch os.Getenv(key) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be exact true or false", key)
	}
}
