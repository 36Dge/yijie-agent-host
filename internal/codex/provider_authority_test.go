package codex

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFEAT137CodexHomeLeaseRejectsSecondManagerInProcess(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	first, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireManagedCodexHomeAuthority(home); err == nil ||
		!strings.Contains(err.Error(), "already leased by this Host process") {
		t.Fatalf("same-process CODEX_HOME lease was not exclusive: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatalf("released in-process lease was not reusable: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT137CodexHomeAuthorityPinsPhysicalOwnerOnlyRoot(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := authority.Close(); err != nil {
			t.Error(err)
		}
	}()
	physical, err := filepath.EvalSymlinks(home)
	if err != nil || physical != home {
		t.Fatalf("test CODEX_HOME is not physical canonical: physical=%q err=%v", physical, err)
	}
	pathInfo, err := os.Lstat(home)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(pathInfo, authority.rootInfo) || !currentUserOwns(pathInfo) ||
		!pathInfo.IsDir() || pathInfo.Mode().Perm() != 0o700 || pathInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("authority did not pin the physical owner-only root: path=%v root=%v", pathInfo, authority.rootInfo)
	}
	identity, err := managedCodexHomeFileIdentity(pathInfo)
	if err != nil || identity != authority.rootIdentity {
		t.Fatalf("authority root device/inode identity drifted: got=%+v want=%+v err=%v", identity, authority.rootIdentity, err)
	}
}

func TestFEAT137CodexHomeLeaseIsReleasedByNormalProcessExit(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(testBinary, "-test.run=^TestFEAT137CodexHomeLeaseHelper$", "-test.count=1")
	command.Env = append(os.Environ(),
		"GO_WANT_FEAT137_LEASE_HELPER=1",
		"YIJIE_FEAT137_LEASE_HOME="+home,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(stdout)
	line, err := reader.ReadString('\n')
	if err != nil || line != "leased\n" {
		_ = stdin.Close()
		_ = command.Wait()
		t.Fatalf("lease helper did not acquire authority: line=%q err=%v", line, err)
	}
	if _, err := acquireManagedCodexHomeAuthority(home); err == nil ||
		!strings.Contains(err.Error(), "already leased by another Host") {
		_ = stdin.Close()
		_ = command.Wait()
		t.Fatalf("cross-process CODEX_HOME lease was not exclusive: %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatalf("lease helper did not exit normally: %v", err)
	}
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatalf("operating system did not release lease on normal process exit: %v", err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT137CodexHomeLeaseHelper(t *testing.T) {
	if os.Getenv("GO_WANT_FEAT137_LEASE_HELPER") != "1" {
		return
	}
	authority, err := acquireManagedCodexHomeAuthority(os.Getenv("YIJIE_FEAT137_LEASE_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	// Deliberately keep authority live until this helper exits normally. This
	// proves the OS releases the CLOEXEC lease without unlinking a lock file.
	_ = authority
	if _, err := fmt.Fprintln(os.Stdout, "leased"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT137LegacyRuleIsMigratedAndCleanedOnlyWhenExact(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "gate on migration", false: "gate off cleanup"}[enabled], func(t *testing.T) {
			home := canonicalOwnedTempDir(t)
			rulesDirectory := filepath.Join(home, managedRulesDirectory)
			if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			legacyPath := filepath.Join(rulesDirectory, managedLegacyDefaultRulesFile)
			if err := os.WriteFile(legacyPath, []byte(managedFEAT137ExecPolicy), 0o600); err != nil {
				t.Fatal(err)
			}
			authority, err := acquireManagedCodexHomeAuthority(home)
			if err != nil {
				t.Fatal(err)
			}
			if enabled {
				if err := prepareMiniMaxCodexHomeForAuthority(
					authority, ManagedReasoningProfileHighRaw, true,
				); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := authority.preflightFEAT137ExecPolicy()
			if err != nil {
				t.Fatal(err)
			}
			if err := authority.applyFEAT137ExecPolicy(enabled, plan); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(legacyPath); !os.IsNotExist(err) {
				t.Fatalf("exact legacy policy was not removed: %v", err)
			}
			managedPath := filepath.Join(rulesDirectory, managedFEAT137RulesFile)
			if enabled {
				content, err := os.ReadFile(managedPath)
				if err != nil || !bytes.Equal(content, []byte(managedFEAT137ExecPolicy)) {
					t.Fatalf("legacy policy was not migrated exactly: content=%q err=%v", content, err)
				}
			} else if _, err := os.Lstat(managedPath); !os.IsNotExist(err) {
				t.Fatalf("gate-off created a managed policy: %v", err)
			}
			if err := authority.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(managedPath); !os.IsNotExist(err) {
				t.Fatalf("authority close retained the Runtime rule: %v", err)
			}
		})
	}
}

func TestFEAT137GateOffRejectsMarkerPrefixDriftWithoutMutation(t *testing.T) {
	for _, name := range []string{managedLegacyDefaultRulesFile, managedFEAT137RulesFile} {
		t.Run(name, func(t *testing.T) {
			home := canonicalOwnedTempDir(t)
			rulesDirectory := filepath.Join(home, managedRulesDirectory)
			if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(rulesDirectory, name)
			drifted := []byte(managedFEAT137ExecPolicy + "# appended benign drift\n")
			if err := os.WriteFile(path, drifted, 0o600); err != nil {
				t.Fatal(err)
			}
			authority, err := acquireManagedCodexHomeAuthority(home)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := authority.preflightFEAT137ExecPolicy(); err == nil {
				t.Fatal("gate-off accepted marker-prefix drift")
			}
			if err := authority.Close(); err == nil {
				t.Fatal("authority cleanup accepted marker-prefix drift")
			}
			content, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, drifted) {
				t.Fatalf("gate-off mutated drifted policy: content=%q err=%v", content, err)
			}
		})
	}
}

func TestFEAT137RejectsUnexpectedSecondRuleWithoutMutation(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	rulesDirectory := filepath.Join(home, managedRulesDirectory)
	if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	managedPath := filepath.Join(rulesDirectory, managedFEAT137RulesFile)
	if err := os.WriteFile(managedPath, []byte(managedFEAT137ExecPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(rulesDirectory, "other.rules")
	second := []byte("# unrelated benign rule\n")
	if err := os.WriteFile(secondPath, second, 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := authority.preflightFEAT137ExecPolicy(); err == nil {
		t.Fatal("unexpected second Runtime rule was accepted")
	}
	if err := authority.Close(); err == nil {
		t.Fatal("cleanup accepted unexpected second Runtime rule")
	}
	for path, want := range map[string][]byte{
		managedPath: []byte(managedFEAT137ExecPolicy),
		secondPath:  second,
	} {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("unexpected rule handling mutated %s: content=%q err=%v", filepath.Base(path), got, err)
		}
	}
}

func TestFEAT137GateOffFakeProviderCleansExactStaleRule(t *testing.T) {
	home := canonicalOwnedTempDir(t)
	rulesDirectory := filepath.Join(home, managedRulesDirectory)
	if err := os.Mkdir(rulesDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	rulePath := filepath.Join(rulesDirectory, managedFEAT137RulesFile)
	if err := os.WriteFile(rulePath, []byte(managedFEAT137ExecPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	authority, err := acquireManagedCodexHomeAuthority(home)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := authority.preflightFEAT137ExecPolicy()
	if err != nil {
		t.Fatal(err)
	}
	fake := FakeResponsesConfig{
		Enabled: true, BaseURL: FEAT126FakeBaseURL,
		RunID: "123e4567-e89b-42d3-a456-426614174000", FixtureID: FEAT126FakeFixtureID,
	}
	if err := prepareFakeResponsesCodexHome(authority, fake); err != nil {
		t.Fatal(err)
	}
	if err := authority.applyFEAT137ExecPolicy(false, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(rulePath); !os.IsNotExist(err) {
		t.Fatalf("gate-off Fake provider retained stale FEAT-137 rule: %v", err)
	}
	if err := authority.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestFEAT137RuleApplyRequiresFinalExactPostcondition(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(string) error
	}{
		{
			name: "second benign rule appears",
			mutate: func(rulesDirectory string) error {
				return os.WriteFile(filepath.Join(rulesDirectory, "concurrent-benign.rules"), []byte("# benign concurrent config\n"), 0o600)
			},
		},
		{
			name: "managed content changes",
			mutate: func(rulesDirectory string) error {
				return os.WriteFile(
					filepath.Join(rulesDirectory, managedFEAT137RulesFile),
					[]byte(managedFEAT137ExecPolicy+"# benign concurrent drift\n"),
					0o600,
				)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := canonicalOwnedTempDir(t)
			authority, err := acquireManagedCodexHomeAuthority(home)
			if err != nil {
				t.Fatal(err)
			}
			if err := prepareMiniMaxCodexHomeForAuthority(
				authority, ManagedReasoningProfileHighRaw, true,
			); err != nil {
				t.Fatal(err)
			}
			plan, err := authority.preflightFEAT137ExecPolicy()
			if err != nil {
				t.Fatal(err)
			}
			rulesDirectory := filepath.Join(home, managedRulesDirectory)
			authority.testBeforeFEAT137Postcondition = func() error {
				return test.mutate(rulesDirectory)
			}
			if err := authority.applyFEAT137ExecPolicy(true, plan); err == nil {
				t.Fatal("rule apply accepted a non-exact final postcondition")
			}
			if authority.approvalExpected {
				t.Fatal("approvalExpected was set before the exact final postcondition")
			}
			authority.testBeforeFEAT137Postcondition = nil
			if err := authority.releaseWithoutCleanup(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFEAT137RuleApplyPinsPreflightDirectoryIdentity(t *testing.T) {
	first := canonicalOwnedTempDir(t)
	second := canonicalOwnedTempDir(t)
	firstInfo, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	secondInfo, err := os.Stat(second)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		preflight feat137RulePlan
		opened    os.FileInfo
		created   bool
		wantError bool
	}{
		{
			name:      "existing exact inode",
			preflight: feat137RulePlan{directoryExists: true, directoryInfo: firstInfo},
			opened:    firstInfo,
		},
		{
			name:      "existing replacement inode",
			preflight: feat137RulePlan{directoryExists: true, directoryInfo: firstInfo},
			opened:    secondInfo,
			wantError: true,
		},
		{
			name:      "fresh directory created by apply",
			preflight: feat137RulePlan{},
			opened:    firstInfo,
			created:   true,
		},
		{
			name:      "foreign directory appeared after preflight",
			preflight: feat137RulePlan{},
			opened:    secondInfo,
			wantError: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateFEAT137RulesDirectoryTransition(test.preflight, test.opened, test.created)
			if (err != nil) != test.wantError {
				t.Fatalf("directory identity result: err=%v wantError=%v", err, test.wantError)
			}
		})
	}
}

func TestFEAT137ExactRuleRemovalSyncsParentDirectory(t *testing.T) {
	parentPath := canonicalOwnedTempDir(t)
	parent, err := os.Open(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	const name = managedFEAT137RulesFile
	content := []byte(managedFEAT137ExecPolicy)
	if err := os.WriteFile(filepath.Join(parentPath, name), content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, info, exists, err := readOwnedRegularAt(parent, name, int64(len(content)))
	if err != nil || !exists {
		t.Fatalf("read exact removal fixture: exists=%v err=%v", exists, err)
	}
	syncCalls := 0
	if err := removeExactOwnedRegularAt(parent, name, content, info, func(directory *os.File) error {
		syncCalls++
		return directory.Sync()
	}); err != nil {
		t.Fatal(err)
	}
	if syncCalls != 1 {
		t.Fatalf("exact unlink parent sync cardinality = %d, want 1", syncCalls)
	}
	if _, err := os.Lstat(filepath.Join(parentPath, name)); !os.IsNotExist(err) {
		t.Fatalf("exact managed rule remained after synced unlink: %v", err)
	}
}
