package codex

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

const (
	managedRulesDirectory         = "rules"
	managedLegacyDefaultRulesFile = "default.rules"
	managedFEAT137RulesFile       = "yijie-feat-137-command-approval.rules"
	maxManagedAuthorityFileBytes  = 64 << 10
)

// managedCodexHomeAuthority holds an advisory, process-scoped exclusive lock
// on the physical CODEX_HOME directory itself. The descriptor is CLOEXEC, so
// Runtime cannot inherit the lease; the operating system releases it if Host
// exits. Every Host profile takes the same lease, including gate-off and fake
// profiles, and a live Runtime keeps it until waitForExit has observed process
// termination. Runtime may therefore reload *.rules for every new session
// without another conforming Host deleting or replacing its authority.
type managedCodexHomeAuthority struct {
	mu sync.Mutex

	path             string
	root             *os.File
	rootInfo         os.FileInfo
	rootIdentity     managedCodexHomeIdentity
	registryHeld     bool
	approvalExpected bool
	closed           bool

	// Deterministic safe test seams. Production always uses File.Sync and has
	// no callback between mutation and the final exact postcondition.
	testSyncDirectory              func(*os.File) error
	testBeforeFEAT137Postcondition func() error
}

type managedCodexHomeIdentity struct {
	device uint64
	inode  uint64
}

var managedCodexHomeRegistry = struct {
	sync.Mutex
	held map[managedCodexHomeIdentity]struct{}
}{held: make(map[managedCodexHomeIdentity]struct{})}

type managedFilePlan struct {
	name            string
	desired         []byte
	allowedExisting [][]byte
	existed         bool
	existingInfo    os.FileInfo
}

type feat137RulePlan struct {
	directoryExists bool
	directoryInfo   os.FileInfo
	legacyExists    bool
	legacyInfo      os.FileInfo
	managedExists   bool
	managedInfo     os.FileInfo
}

func validateManagedCodexHome(path string) error {
	root, _, err := openManagedCodexHome(path)
	if root != nil {
		_ = root.Close()
	}
	return err
}

func acquireManagedCodexHomeAuthority(path string) (*managedCodexHomeAuthority, error) {
	root, info, err := openManagedCodexHome(path)
	if err != nil {
		return nil, err
	}
	identity, err := managedCodexHomeFileIdentity(info)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	managedCodexHomeRegistry.Lock()
	if _, held := managedCodexHomeRegistry.held[identity]; held {
		managedCodexHomeRegistry.Unlock()
		_ = root.Close()
		return nil, errors.New("managed CODEX_HOME is already leased by this Host process")
	}
	managedCodexHomeRegistry.held[identity] = struct{}{}
	managedCodexHomeRegistry.Unlock()
	if err := unix.Flock(int(root.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		managedCodexHomeRegistry.Lock()
		delete(managedCodexHomeRegistry.held, identity)
		managedCodexHomeRegistry.Unlock()
		_ = root.Close()
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errors.New("managed CODEX_HOME is already leased by another Host")
		}
		return nil, fmt.Errorf("lease managed CODEX_HOME: %w", err)
	}
	authority := &managedCodexHomeAuthority{
		path: path, root: root, rootInfo: info, rootIdentity: identity, registryHeld: true,
	}
	if err := authority.validateRootPathLocked(); err != nil {
		authority.unlockAndCloseRoot()
		return nil, err
	}
	return authority, nil
}

func openManagedCodexHome(path string) (*os.File, os.FileInfo, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, nil, errors.New("managed CODEX_HOME must be a canonical absolute path")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return nil, nil, fmt.Errorf("inspect managed CODEX_HOME: %w", err)
	}
	if err := validateOwnedDirectory(pathInfo, "managed CODEX_HOME"); err != nil {
		return nil, nil, err
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve managed CODEX_HOME: %w", err)
	}
	if physical != path {
		return nil, nil, errors.New("managed CODEX_HOME must use its physical canonical path")
	}

	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open managed CODEX_HOME without symlink traversal: %w", err)
	}
	root := os.NewFile(uintptr(fd), "managed CODEX_HOME")
	if root == nil {
		_ = unix.Close(fd)
		return nil, nil, errors.New("open managed CODEX_HOME descriptor")
	}
	rootInfo, err := root.Stat()
	if err != nil {
		_ = root.Close()
		return nil, nil, fmt.Errorf("stat managed CODEX_HOME descriptor: %w", err)
	}
	if err := validateOwnedDirectory(rootInfo, "managed CODEX_HOME descriptor"); err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	if !os.SameFile(pathInfo, rootInfo) {
		_ = root.Close()
		return nil, nil, errors.New("managed CODEX_HOME changed while opening its authority")
	}
	return root, rootInfo, nil
}

func managedCodexHomeFileIdentity(info os.FileInfo) (managedCodexHomeIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return managedCodexHomeIdentity{}, errors.New("managed CODEX_HOME identity is unavailable")
	}
	return managedCodexHomeIdentity{device: uint64(stat.Dev), inode: uint64(stat.Ino)}, nil
}

func validateOwnedDirectory(info os.FileInfo, label string) error {
	if !currentUserOwns(info) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s must be an owner-only non-symlink directory", label)
	}
	return nil
}

func validateOwnedRegularFile(info os.FileInfo, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || !currentUserOwns(info) || !info.Mode().IsRegular() ||
		info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("%s must be an owner-only, single-link regular file", label)
	}
	return nil
}

func (a *managedCodexHomeAuthority) validateRootPathLocked() error {
	if a == nil || a.root == nil || a.rootInfo == nil {
		return errors.New("managed CODEX_HOME authority is unavailable")
	}
	pathInfo, err := os.Lstat(a.path)
	if err != nil {
		return fmt.Errorf("reinspect managed CODEX_HOME: %w", err)
	}
	if err := validateOwnedDirectory(pathInfo, "managed CODEX_HOME"); err != nil {
		return err
	}
	physical, err := filepath.EvalSymlinks(a.path)
	if err != nil || physical != a.path || !os.SameFile(pathInfo, a.rootInfo) {
		return errors.New("managed CODEX_HOME physical authority changed")
	}
	return nil
}

func (a *managedCodexHomeAuthority) preflightManagedFile(
	name string,
	desired []byte,
	allowedExisting ...[]byte,
) (managedFilePlan, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return managedFilePlan{}, errors.New("managed CODEX_HOME authority is closed")
	}
	if err := a.validateRootPathLocked(); err != nil {
		return managedFilePlan{}, err
	}
	if err := validateManagedChildName(name); err != nil {
		return managedFilePlan{}, err
	}
	content, info, exists, err := readOwnedRegularAt(a.root, name, maxManagedAuthorityFileBytes)
	if err != nil {
		return managedFilePlan{}, err
	}
	if exists && !bytes.Equal(content, desired) && !matchesExactBytes(content, allowedExisting) {
		return managedFilePlan{}, fmt.Errorf("refusing to replace unowned or drifted managed file %s", name)
	}
	return managedFilePlan{
		name: name, desired: append([]byte(nil), desired...), allowedExisting: cloneByteSlices(allowedExisting),
		existed: exists, existingInfo: info,
	}, nil
}

func (a *managedCodexHomeAuthority) applyManagedFile(plan managedFilePlan) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("managed CODEX_HOME authority is closed")
	}
	if err := a.validateRootPathLocked(); err != nil {
		return err
	}
	content, info, exists, err := readOwnedRegularAt(a.root, plan.name, maxManagedAuthorityFileBytes)
	if err != nil {
		return err
	}
	if exists != plan.existed || (exists && (!os.SameFile(info, plan.existingInfo) ||
		(!bytes.Equal(content, plan.desired) && !matchesExactBytes(content, plan.allowedExisting)))) {
		return fmt.Errorf("managed file %s changed after preflight", plan.name)
	}
	if exists && bytes.Equal(content, plan.desired) {
		return nil
	}
	if exists {
		return replaceExactOwnedRegularAt(
			a.root, plan.name, content, info, plan.desired, a.syncDirectoryLocked,
		)
	}
	return writeOwnedRegularAt(a.root, plan.name, plan.desired, a.syncDirectoryLocked)
}

func (a *managedCodexHomeAuthority) validateManagedFileExact(name string, expected []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("managed CODEX_HOME authority is closed")
	}
	if err := a.validateRootPathLocked(); err != nil {
		return err
	}
	if err := validateManagedChildName(name); err != nil {
		return err
	}
	content, _, exists, err := readOwnedRegularAt(a.root, name, maxManagedAuthorityFileBytes)
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(content, expected) {
		return fmt.Errorf("managed file %s failed exact authority validation", name)
	}
	return nil
}

func (a *managedCodexHomeAuthority) preflightFEAT137ExecPolicy() (feat137RulePlan, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return feat137RulePlan{}, errors.New("managed CODEX_HOME authority is closed")
	}
	if err := a.validateRootPathLocked(); err != nil {
		return feat137RulePlan{}, err
	}
	return a.preflightFEAT137ExecPolicyLocked()
}

func (a *managedCodexHomeAuthority) preflightFEAT137ExecPolicyLocked() (feat137RulePlan, error) {
	directory, directoryInfo, exists, _, err := openOwnedDirectoryAt(a.root, managedRulesDirectory, false)
	if err != nil {
		return feat137RulePlan{}, err
	}
	if !exists {
		if a.approvalExpected {
			return feat137RulePlan{}, errors.New("live FEAT-137 Runtime exec policy directory disappeared")
		}
		return feat137RulePlan{}, nil
	}
	defer directory.Close()
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return feat137RulePlan{}, fmt.Errorf("read managed Runtime rules directory: %w", err)
	}
	plan := feat137RulePlan{directoryExists: true, directoryInfo: directoryInfo}
	for _, name := range names {
		switch name {
		case managedLegacyDefaultRulesFile:
			content, info, present, readErr := readOwnedRegularAt(directory, name, 4096)
			if readErr != nil {
				return feat137RulePlan{}, readErr
			}
			if !present || !bytes.Equal(content, []byte(managedFEAT137ExecPolicy)) {
				return feat137RulePlan{}, errors.New("legacy FEAT-137 Runtime exec policy is not exact-owned")
			}
			plan.legacyExists, plan.legacyInfo = true, info
		case managedFEAT137RulesFile:
			content, info, present, readErr := readOwnedRegularAt(directory, name, 4096)
			if readErr != nil {
				return feat137RulePlan{}, readErr
			}
			if !present || !bytes.Equal(content, []byte(managedFEAT137ExecPolicy)) {
				return feat137RulePlan{}, errors.New("managed FEAT-137 Runtime exec policy drifted")
			}
			plan.managedExists, plan.managedInfo = true, info
		default:
			return feat137RulePlan{}, errors.New("managed Runtime rules directory contains an unexpected entry")
		}
	}
	if a.approvalExpected && !plan.managedExists {
		return feat137RulePlan{}, errors.New("live FEAT-137 Runtime exec policy authority disappeared")
	}
	return plan, nil
}

func (a *managedCodexHomeAuthority) applyFEAT137ExecPolicy(enabled bool, plan feat137RulePlan) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return errors.New("managed CODEX_HOME authority is closed")
	}
	return a.applyFEAT137ExecPolicyLocked(enabled, plan)
}

func (a *managedCodexHomeAuthority) Close() error {
	return a.close(true)
}

// releaseWithoutCleanup is used only when a concurrent Shutdown wins before
// Start has entered the serialized CODEX_HOME mutation phase. It releases the
// read-only lease without reconciling any stale rule, preserving the guarantee
// that the losing startup does not mutate provider state.
func (a *managedCodexHomeAuthority) releaseWithoutCleanup() error {
	return a.close(false)
}

func (a *managedCodexHomeAuthority) close(cleanup bool) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	var cleanupErr error
	if cleanup {
		var cleanupPlan feat137RulePlan
		cleanupPlan, cleanupErr = a.preflightFEAT137ExecPolicyLocked()
		if cleanupErr == nil {
			// applyFEAT137ExecPolicy takes the same mutex, so perform the closed
			// cleanup inline while preserving the lease.
			cleanupErr = a.applyFEAT137ExecPolicyLocked(false, cleanupPlan)
		}
	}
	a.closed = true
	root := a.root
	a.root = nil
	a.mu.Unlock()

	var releaseErr error
	if root != nil {
		if err := unix.Flock(int(root.Fd()), unix.LOCK_UN); err != nil {
			releaseErr = fmt.Errorf("unlock managed CODEX_HOME: %w", err)
		}
		if err := root.Close(); err != nil && releaseErr == nil {
			releaseErr = fmt.Errorf("close managed CODEX_HOME authority: %w", err)
		}
	}
	if a.registryHeld {
		managedCodexHomeRegistry.Lock()
		delete(managedCodexHomeRegistry.held, a.rootIdentity)
		managedCodexHomeRegistry.Unlock()
		a.registryHeld = false
	}
	return errors.Join(cleanupErr, releaseErr)
}

func (a *managedCodexHomeAuthority) applyFEAT137ExecPolicyLocked(enabled bool, plan feat137RulePlan) error {
	if err := a.validateRootPathLocked(); err != nil {
		return err
	}
	if enabled {
		if err := a.validateFEAT137ClosedConfigLocked(); err != nil {
			return err
		}
	}
	current, err := a.preflightFEAT137ExecPolicyLocked()
	if err != nil {
		return err
	}
	if !sameFEAT137RulePlan(plan, current) {
		return errors.New("managed FEAT-137 Runtime exec policy changed after preflight")
	}
	directory, directoryInfo, exists, created, err := openOwnedDirectoryAt(a.root, managedRulesDirectory, enabled)
	if err != nil {
		return err
	}
	if !exists {
		if enabled {
			return errors.New("managed FEAT-137 Runtime rules directory was not created")
		}
		if err := a.verifyFEAT137RulesDirectoryAbsentLocked(); err != nil {
			return err
		}
		a.approvalExpected = false
		return nil
	}
	defer directory.Close()
	if err := validateFEAT137RulesDirectoryTransition(current, directoryInfo, created); err != nil {
		return err
	}
	if !current.directoryExists {
		// When preflight observed no directory, only the directory created by this
		// exact apply operation is admissible. An entry that appeared between the
		// final preflight and open is foreign authority even if its mode and owner
		// happen to be valid.
		if err := a.syncDirectoryLocked(a.root); err != nil {
			return fmt.Errorf("sync managed CODEX_HOME after creating Runtime rules directory: %w", err)
		}
	}
	if enabled {
		if !current.managedExists {
			if err := writeOwnedRegularAt(
				directory, managedFEAT137RulesFile, []byte(managedFEAT137ExecPolicy), a.syncDirectoryLocked,
			); err != nil {
				return fmt.Errorf("write managed FEAT-137 Runtime exec policy: %w", err)
			}
		}
		if current.legacyExists {
			if err := removeExactOwnedRegularAt(directory, managedLegacyDefaultRulesFile,
				[]byte(managedFEAT137ExecPolicy), current.legacyInfo, a.syncDirectoryLocked); err != nil {
				return fmt.Errorf("migrate legacy FEAT-137 Runtime exec policy: %w", err)
			}
		}
		if err := a.verifyFEAT137RulesPostconditionLocked(directory, directoryInfo, true); err != nil {
			return err
		}
		a.approvalExpected = true
		return nil
	}
	if current.legacyExists {
		if err := removeExactOwnedRegularAt(directory, managedLegacyDefaultRulesFile,
			[]byte(managedFEAT137ExecPolicy), current.legacyInfo, a.syncDirectoryLocked); err != nil {
			return fmt.Errorf("remove disabled legacy FEAT-137 Runtime exec policy: %w", err)
		}
	}
	if current.managedExists {
		if err := removeExactOwnedRegularAt(directory, managedFEAT137RulesFile,
			[]byte(managedFEAT137ExecPolicy), current.managedInfo, a.syncDirectoryLocked); err != nil {
			return fmt.Errorf("remove disabled FEAT-137 Runtime exec policy: %w", err)
		}
	}
	if err := a.verifyFEAT137RulesPostconditionLocked(directory, directoryInfo, false); err != nil {
		return err
	}
	a.approvalExpected = false
	// See applyFEAT137ExecPolicy: the empty owner-only directory is inert and is
	// intentionally retained to avoid a pathname-only directory unlink race.
	return nil
}

func (a *managedCodexHomeAuthority) validateFEAT137ClosedConfigLocked() error {
	content, _, exists, err := readOwnedRegularAt(a.root, "config.toml", maxManagedAuthorityFileBytes)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("FEAT-137 Runtime exec policy requires the closed managed MiniMax config")
	}
	defaultConfig, err := miniMaxManagedConfigForAuthority(
		a.path, ManagedReasoningProfileDefault, true,
	)
	if err != nil {
		return err
	}
	highRawConfig, err := miniMaxManagedConfigForAuthority(
		a.path, ManagedReasoningProfileHighRaw, true,
	)
	if err != nil {
		return err
	}
	if !bytes.Equal(content, defaultConfig) && !bytes.Equal(content, highRawConfig) {
		return errors.New("FEAT-137 Runtime exec policy requires the exact closed managed MiniMax config")
	}
	return validateFEAT137ManagedConfig(content, true)
}

func validateFEAT137RulesDirectoryTransition(
	preflight feat137RulePlan,
	opened os.FileInfo,
	created bool,
) error {
	if opened == nil {
		return errors.New("managed FEAT-137 Runtime rules directory identity is unavailable")
	}
	if preflight.directoryExists {
		if created || preflight.directoryInfo == nil || !os.SameFile(opened, preflight.directoryInfo) {
			return errors.New("managed FEAT-137 Runtime rules directory changed after preflight")
		}
		return nil
	}
	if !created {
		return errors.New("managed FEAT-137 Runtime rules directory appeared after preflight")
	}
	return nil
}

func (a *managedCodexHomeAuthority) verifyFEAT137RulesDirectoryAbsentLocked() error {
	if a.testBeforeFEAT137Postcondition != nil {
		if err := a.testBeforeFEAT137Postcondition(); err != nil {
			return fmt.Errorf("FEAT-137 Runtime rule postcondition hook: %w", err)
		}
	}
	directory, _, exists, _, err := openOwnedDirectoryAt(a.root, managedRulesDirectory, false)
	if directory != nil {
		_ = directory.Close()
	}
	if err != nil {
		return err
	}
	if exists {
		return errors.New("FEAT-137 Runtime rules directory appeared after gate-off preflight")
	}
	return nil
}

func (a *managedCodexHomeAuthority) verifyFEAT137RulesPostconditionLocked(
	directory *os.File,
	directoryInfo os.FileInfo,
	enabled bool,
) error {
	if a.testBeforeFEAT137Postcondition != nil {
		if err := a.testBeforeFEAT137Postcondition(); err != nil {
			return fmt.Errorf("FEAT-137 Runtime rule postcondition hook: %w", err)
		}
	}
	if err := ensureDirectoryEntrySame(a.root, managedRulesDirectory, directoryInfo); err != nil {
		return err
	}
	names, err := directory.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("re-enumerate managed Runtime rules directory: %w", err)
	}
	if !enabled {
		if len(names) != 0 {
			return errors.New("gate-off Runtime rules directory is not empty after exact cleanup")
		}
		return ensureDirectoryEntrySame(a.root, managedRulesDirectory, directoryInfo)
	}
	if len(names) != 1 || names[0] != managedFEAT137RulesFile {
		return errors.New("gate-on Runtime rules directory does not contain only the exact managed authority")
	}
	content, ruleInfo, exists, err := readOwnedRegularAt(directory, managedFEAT137RulesFile, 4096)
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(content, []byte(managedFEAT137ExecPolicy)) {
		return errors.New("gate-on Runtime rule postcondition is not exact")
	}
	if err := ensureRegularEntrySame(directory, managedFEAT137RulesFile, ruleInfo); err != nil {
		return err
	}
	return ensureDirectoryEntrySame(a.root, managedRulesDirectory, directoryInfo)
}

func (a *managedCodexHomeAuthority) syncDirectoryLocked(directory *os.File) error {
	if a.testSyncDirectory != nil {
		return a.testSyncDirectory(directory)
	}
	return directory.Sync()
}

func (a *managedCodexHomeAuthority) unlockAndCloseRoot() {
	if a.root == nil {
		return
	}
	_ = unix.Flock(int(a.root.Fd()), unix.LOCK_UN)
	_ = a.root.Close()
	a.root = nil
	if a.registryHeld {
		managedCodexHomeRegistry.Lock()
		delete(managedCodexHomeRegistry.held, a.rootIdentity)
		managedCodexHomeRegistry.Unlock()
		a.registryHeld = false
	}
}

func openOwnedDirectoryAt(parent *os.File, name string, create bool) (*os.File, os.FileInfo, bool, bool, error) {
	if err := validateManagedChildName(name); err != nil {
		return nil, nil, false, false, err
	}
	created := false
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) && create {
		if err := unix.Mkdirat(int(parent.Fd()), name, 0o700); err != nil {
			if !errors.Is(err, unix.EEXIST) {
				return nil, nil, false, false, fmt.Errorf("create managed Runtime rules directory: %w", err)
			}
		} else {
			created = true
		}
		fd, err = unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	if errors.Is(err, unix.ENOENT) {
		return nil, nil, false, false, nil
	}
	if err != nil {
		return nil, nil, false, false, fmt.Errorf("open managed Runtime rules directory: %w", err)
	}
	directory := os.NewFile(uintptr(fd), name)
	info, err := directory.Stat()
	if err != nil {
		_ = directory.Close()
		return nil, nil, false, false, err
	}
	if err := validateOwnedDirectory(info, "managed Runtime rules directory"); err != nil {
		_ = directory.Close()
		return nil, nil, false, false, err
	}
	if err := ensureDirectoryEntrySame(parent, name, info); err != nil {
		_ = directory.Close()
		return nil, nil, false, false, err
	}
	return directory, info, true, created, nil
}

func readOwnedRegularAt(
	parent *os.File,
	name string,
	limit int64,
) ([]byte, os.FileInfo, bool, error) {
	if err := validateManagedChildName(name); err != nil {
		return nil, nil, false, err
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("open managed file %s: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, nil, false, err
	}
	if err := validateOwnedRegularFile(info, "managed file "+name); err != nil {
		return nil, nil, false, err
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, nil, false, fmt.Errorf("read managed file %s: %w", name, err)
	}
	if int64(len(content)) > limit {
		return nil, nil, false, fmt.Errorf("managed file %s exceeds its closed size limit", name)
	}
	if err := ensureRegularEntrySame(parent, name, info); err != nil {
		return nil, nil, false, err
	}
	return content, info, true, nil
}

func writeOwnedRegularAt(
	parent *os.File,
	name string,
	content []byte,
	syncDirectory func(*os.File) error,
) error {
	return publishOwnedRegularAt(parent, name, content, nil, nil, syncDirectory)
}

func replaceExactOwnedRegularAt(
	parent *os.File,
	name string,
	expected []byte,
	expectedInfo os.FileInfo,
	content []byte,
	syncDirectory func(*os.File) error,
) error {
	return publishOwnedRegularAt(parent, name, content, expected, expectedInfo, syncDirectory)
}

// publishOwnedRegularAt never renames over a pathname. New content is linked
// into an absent destination; replacement first removes only the exact inode
// and bytes observed by the caller. A concurrent child insertion therefore
// produces EEXIST and fails closed instead of being overwritten.
func publishOwnedRegularAt(
	parent *os.File,
	name string,
	content []byte,
	expected []byte,
	expectedInfo os.FileInfo,
	syncDirectory func(*os.File) error,
) (returnErr error) {
	if err := validateManagedChildName(name); err != nil {
		return err
	}
	temporaryName := ".yijie-managed-" + uuid.NewString()
	fd, err := unix.Openat(
		int(parent.Fd()), temporaryName,
		unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create managed temporary file: %w", err)
	}
	file := os.NewFile(uintptr(fd), temporaryName)
	defer func() {
		if err := unix.Unlinkat(int(parent.Fd()), temporaryName, 0); err == nil {
			if syncDirectory == nil {
				returnErr = errors.Join(returnErr, errors.New("managed directory sync authority is required"))
			} else if syncErr := syncDirectory(parent); syncErr != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("sync managed directory after temporary cleanup: %w", syncErr))
			}
		} else if !errors.Is(err, unix.ENOENT) {
			returnErr = errors.Join(returnErr, fmt.Errorf("remove managed temporary file: %w", err))
		}
	}()
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return statErr
	}
	if err := validateOwnedRegularFile(info, "managed temporary file"); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if expectedInfo != nil {
		if err := removeExactOwnedRegularAt(parent, name, expected, expectedInfo, syncDirectory); err != nil {
			return err
		}
	} else if _, _, exists, err := readOwnedRegularAt(parent, name, maxManagedAuthorityFileBytes); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("refusing to publish over existing managed file %s", name)
	}
	if err := unix.Linkat(int(parent.Fd()), temporaryName, int(parent.Fd()), name, 0); err != nil {
		return fmt.Errorf("publish managed file %s without replacement: %w", name, err)
	}
	if err := unix.Unlinkat(int(parent.Fd()), temporaryName, 0); err != nil {
		return fmt.Errorf("remove published managed temporary file: %w", err)
	}
	if syncDirectory == nil {
		return errors.New("managed directory sync authority is required")
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync managed directory after publishing %s: %w", name, err)
	}
	actual, _, exists, err := readOwnedRegularAt(parent, name, int64(len(content)))
	if err != nil {
		return err
	}
	if !exists || !bytes.Equal(actual, content) {
		return fmt.Errorf("managed file %s failed exact post-write verification", name)
	}
	return nil
}

func removeExactOwnedRegularAt(
	parent *os.File,
	name string,
	expected []byte,
	expectedInfo os.FileInfo,
	syncDirectory func(*os.File) error,
) error {
	content, info, exists, err := readOwnedRegularAt(parent, name, int64(len(expected)))
	if err != nil {
		return err
	}
	if !exists || expectedInfo == nil || !os.SameFile(info, expectedInfo) || !bytes.Equal(content, expected) {
		return fmt.Errorf("refusing to remove non-exact managed file %s", name)
	}
	if err := unix.Unlinkat(int(parent.Fd()), name, 0); err != nil {
		return fmt.Errorf("remove exact managed file %s: %w", name, err)
	}
	if syncDirectory == nil {
		return errors.New("managed directory sync authority is required")
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync managed directory after removing %s: %w", name, err)
	}
	return nil
}

func ensureDirectoryEntrySame(parent *os.File, name string, expected os.FileInfo) error {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen managed directory %s: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := validateOwnedDirectory(info, "managed directory "+name); err != nil {
		return err
	}
	if !os.SameFile(info, expected) {
		return fmt.Errorf("managed directory %s changed during operation", name)
	}
	return nil
}

func ensureRegularEntrySame(parent *os.File, name string, expected os.FileInfo) error {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("reopen managed file %s: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := validateOwnedRegularFile(info, "managed file "+name); err != nil {
		return err
	}
	if !os.SameFile(info, expected) {
		return fmt.Errorf("managed file %s changed during operation", name)
	}
	return nil
}

func validateManagedChildName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsRune(name, os.PathSeparator) {
		return errors.New("managed CODEX_HOME child name is invalid")
	}
	return nil
}

func matchesExactBytes(content []byte, candidates [][]byte) bool {
	for _, candidate := range candidates {
		if bytes.Equal(content, candidate) {
			return true
		}
	}
	return false
}

func cloneByteSlices(values [][]byte) [][]byte {
	cloned := make([][]byte, len(values))
	for index := range values {
		cloned[index] = append([]byte(nil), values[index]...)
	}
	return cloned
}

func sameFEAT137RulePlan(left, right feat137RulePlan) bool {
	if left.directoryExists != right.directoryExists || left.legacyExists != right.legacyExists ||
		left.managedExists != right.managedExists {
		return false
	}
	for _, pair := range [][2]os.FileInfo{
		{left.directoryInfo, right.directoryInfo},
		{left.legacyInfo, right.legacyInfo},
		{left.managedInfo, right.managedInfo},
	} {
		if (pair[0] == nil) != (pair[1] == nil) || (pair[0] != nil && !os.SameFile(pair[0], pair[1])) {
			return false
		}
	}
	return true
}
