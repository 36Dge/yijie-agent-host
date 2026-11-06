package codex

import (
	"errors"
	"io"
	"os"

	"github.com/google/uuid"
	"golang.org/x/sys/unix"
)

// An inert comment distinguishes the native append target from the retired
// FEAT-137 fixture which used the same filename. Only Codex writes/evaluates
// rule contents. Never adopt an unmarked file or change existing permissions.
const nativeCommandRulesOwner = ".yijie-native-command-rules-v1"

// This Home-local ownership marker contains no command, grant, or policy. It
// prevents an unrelated file with a familiar comment from being adopted.
func (a *managedCodexHomeAuthority) nativeRulesHeader(create bool) (string, error) {
	owner, _, exists, err := readOwnedRegularAt(a.root, nativeCommandRulesOwner, 36)
	if err != nil {
		return "", err
	}
	if !exists {
		if !create {
			return "", nil
		}
		owner = []byte(uuid.NewString())
		if err := writeOwnedRegularAt(a.root, nativeCommandRulesOwner, owner, a.syncDirectoryLocked); err != nil {
			return "", err
		}
	}
	id, err := uuid.Parse(string(owner))
	if err != nil || id.String() != string(owner) || id == uuid.Nil {
		return "", errors.New("invalid native command rules owner")
	}
	return "# yijie-codex native command approvals v1: " + string(owner) + "\n", nil
}

func (a *managedCodexHomeAuthority) inspectNativeCommandRules(directory *os.File) (os.FileInfo, bool, error) {
	expected, err := a.nativeRulesHeader(false)
	if err != nil || expected == "" {
		return nil, false, err
	}
	name := managedLegacyDefaultRulesFile
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, false, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if err := validateOwnedRegularFile(info, "native Runtime rules file"); err != nil {
		return nil, false, err
	}
	header := make([]byte, len(expected))
	n, err := io.ReadFull(file, header)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return nil, false, err
	}
	if err := ensureRegularEntrySame(directory, name, info); err != nil {
		return nil, false, err
	}
	return info, string(header[:n]) == expected, nil
}

// Provision the private append target only after an explicit broader choice,
// under the existing Home lease and exclusive-create/identity checks.
func (a *managedCodexHomeAuthority) prepareNativeCommandRules() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || !a.nativeCommandRules || a.approvalExpected {
		return errors.New("native command rules are unavailable")
	}
	if err := a.validateRootPathLocked(); err != nil {
		return err
	}
	plan, err := a.preflightFEAT137ExecPolicyLocked()
	if err != nil {
		return err
	}
	if plan.legacyExists || plan.managedExists {
		return errors.New("legacy command rule authority is still present")
	}
	if plan.nativeInfo != nil {
		return nil
	}
	dir, info, _, created, err := openOwnedDirectoryAt(a.root, managedRulesDirectory, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := validateFEAT137RulesDirectoryTransition(plan, info, created); err != nil {
		return err
	}
	if created {
		if err := a.syncDirectoryLocked(a.root); err != nil {
			return err
		}
	}
	header, err := a.nativeRulesHeader(true)
	if err != nil {
		return err
	}
	return writeOwnedRegularAt(dir, managedLegacyDefaultRulesFile, []byte(header), a.syncDirectoryLocked)
}
