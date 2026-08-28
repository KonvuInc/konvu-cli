package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRecoversEveryTerminalSnapshotAndLeavesTwoArtifacts(t *testing.T) {
	for index, status := range []Status{StatusCompleted, StatusFailed, StatusCancelled} {
		t.Run(string(status), func(t *testing.T) {
			root := t.TempDir()
			id := fmt.Sprintf("payments--01234567--%06d", index+1)
			dir, prepared := writePreparedTerminal(t, root, id, status)
			writeTerminalLog(t, dir, status, baselineFingerprint(prepared), true)

			runs, err := (Store{Root: root}).List()
			if err != nil || len(runs) != 1 || !runs[0].Valid || runs[0].Run.Status != status {
				t.Fatalf("recovered runs = %#v, error = %v", runs, err)
			}
			if _, err := os.Lstat(filepath.Join(dir, preparedBaselineName)); !os.IsNotExist(err) {
				t.Fatalf("prepared snapshot remains after recovery: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 2 {
				t.Fatalf("terminal artifacts = %#v, error = %v", entries, err)
			}
		})
	}
}

func TestStoreLeavesPartialTerminalRecordRunning(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusCompleted)
	writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), false)

	runs, err := (Store{Root: root}).List()
	if err != nil || len(runs) != 1 || !runs[0].Valid || runs[0].Run.Status != StatusRunning {
		t.Fatalf("partial terminal run = %#v, error = %v", runs, err)
	}
	if info, err := os.Lstat(filepath.Join(dir, preparedBaselineName)); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("prepared snapshot was not retained: %v", err)
	}
}

func TestStoreRejectsUnrecoverableTerminalStates(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, string, string)
		want    string
	}{
		{
			name: "fingerprint mismatch",
			prepare: func(t *testing.T, root, id string) {
				dir, _ := writePreparedTerminal(t, root, id, StatusCompleted)
				writeTerminalLog(t, dir, StatusCompleted, "fnv1a64:0000000000000000", true)
			},
			want: "fingerprint does not match",
		},
		{
			name: "missing prepared snapshot",
			prepare: func(t *testing.T, root, id string) {
				dir := writeStoredRun(t, root, storedRunFixture{
					id: id, name: "payments", path: "/repos/payments", status: StatusRunning,
					started: "2026-08-27T10:00:00Z",
				})
				writeTerminalLog(t, dir, StatusCompleted, "fnv1a64:0000000000000000", true)
			},
			want: "no prepared baseline snapshot",
		},
		{
			name: "status mismatch",
			prepare: func(t *testing.T, root, id string) {
				dir, prepared := writePreparedTerminal(t, root, id, StatusFailed)
				writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), true)
			},
			want: "status does not match",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			id := "payments--01234567--000001"
			test.prepare(t, root, id)
			runs, err := (Store{Root: root}).List()
			if err != nil || len(runs) != 1 || runs[0].Valid || !strings.Contains(runs[0].Problem, test.want) {
				t.Fatalf("invalid recovery = %#v, error = %v", runs, err)
			}
		})
	}
}

func TestRecoveryAcceptsAnAlreadyInstalledMatchingSnapshot(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusCompleted)
	writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), true)
	if err := os.Rename(filepath.Join(dir, preparedBaselineName), filepath.Join(dir, "baseline.json")); err != nil {
		t.Fatal(err)
	}
	runRoot, _, err := openRunRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runRoot.Close()
	synced := false
	recovered, err := recoverTerminalSnapshotWithOperations(
		runRoot,
		id,
		func(root *os.Root, name string, file *os.File) error {
			synced = true
			return syncOpenedRootRegularFile(root, name, file)
		},
		func(_, _ string) error {
			return fmt.Errorf("already-installed recovery attempted a rename")
		},
	)
	if err != nil || recovered == nil || !synced {
		t.Fatalf("idempotent recovery = %v, %v", recovered, err)
	}
}

func TestRecoveryFailureRemainsPendingInsteadOfRunning(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusCompleted)
	writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), true)
	if err := os.Remove(filepath.Join(dir, "baseline.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "baseline.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	runRoot, _, err := openRunRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runRoot.Close()
	recovered, err := recoverTerminalSnapshot(runRoot, id)
	if recovered != nil || err == nil || !strings.Contains(err.Error(), "installing prepared baseline") {
		t.Fatalf("recovery = %v, error = %v", recovered, err)
	}
	if info, statErr := os.Lstat(filepath.Join(dir, preparedBaselineName)); statErr != nil || !info.Mode().IsRegular() {
		t.Fatalf("recoverable snapshot was lost: %v", statErr)
	}
}

func TestStoreNeverFollowsASymlinkedPreparedSnapshot(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusCompleted)
	writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), true)
	tmp := filepath.Join(dir, preparedBaselineName)
	outside := filepath.Join(t.TempDir(), "outside.json")
	mustWrite(t, outside, prepared)
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, tmp); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	runs, err := (Store{Root: root}).List()
	if err != nil || len(runs) != 1 || runs[0].Valid || !strings.Contains(runs[0].Problem, "non-symlinked") {
		t.Fatalf("symlinked recovery = %#v, error = %v", runs, err)
	}
}

func TestStoreUsesFirstTerminalFieldsWhenErrorTextContainsFieldTokens(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusFailed)
	line := "2026-08-27T10:02:03Z status=failed baseline_fingerprint=" +
		baselineFingerprint(prepared) +
		" error=upstream status=running baseline_fingerprint=fnv1a64:0000000000000000\n"
	mustWrite(t, filepath.Join(dir, "run.log"), []byte(line))

	runs, err := (Store{Root: root}).List()
	if err != nil || len(runs) != 1 || !runs[0].Valid || runs[0].Run.Status != StatusFailed {
		t.Fatalf("duplicate terminal fields = %#v, error = %v", runs, err)
	}
}

func TestRecoverySyncsTerminalLogBeforeRename(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusCompleted)
	writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), true)
	runRoot, _, err := openRunRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runRoot.Close()

	events := make([]string, 0, 2)
	document, err := recoverTerminalSnapshotWithOperations(
		runRoot,
		id,
		func(root *os.Root, name string, file *os.File) error {
			events = append(events, "sync")
			return syncOpenedRootRegularFile(root, name, file)
		},
		func(oldName, newName string) error {
			events = append(events, "rename")
			return runRoot.Rename(oldName, newName)
		},
	)
	if err != nil || document == nil || document.Run.Status != StatusCompleted {
		t.Fatalf("recovery = %#v, error = %v", document, err)
	}
	if order := strings.Join(events, ","); order != "sync,rename" {
		t.Fatalf("recovery operation order = %q", order)
	}
}

func TestRecoveryCleansPreparedSnapshotWhenConcurrentInstallWins(t *testing.T) {
	root := t.TempDir()
	id := "payments--01234567--000001"
	dir, prepared := writePreparedTerminal(t, root, id, StatusCompleted)
	writeTerminalLog(t, dir, StatusCompleted, baselineFingerprint(prepared), true)
	runRoot, _, err := openRunRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer runRoot.Close()

	document, err := recoverTerminalSnapshotWithRename(
		runRoot,
		id,
		func(oldName, newName string) error {
			data, readErr := readRootRegularFile(runRoot, oldName)
			if readErr != nil {
				return readErr
			}
			if writeErr := runRoot.WriteFile(newName, data, 0o600); writeErr != nil {
				return writeErr
			}
			return fmt.Errorf("injected rename failure after concurrent install")
		},
	)
	if err != nil || document == nil || document.Run.Status != StatusCompleted {
		t.Fatalf("concurrent recovery = %#v, error = %v", document, err)
	}
	if _, err := runRoot.Lstat(preparedBaselineName); !os.IsNotExist(err) {
		t.Fatalf("redundant prepared snapshot remains: %v", err)
	}

	runs, err := (Store{Root: root}).List()
	if err != nil || len(runs) != 1 || !runs[0].Valid || runs[0].Run.Status != StatusCompleted {
		t.Fatalf("idempotent listing after concurrent recovery = %#v, error = %v", runs, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("terminal artifacts = %#v, error = %v", entries, err)
	}
}

func TestBaselineFingerprintMatchesRustWireValue(t *testing.T) {
	if got := baselineFingerprint([]byte("baseline\n")); got != "fnv1a64:4555a09346f3e3b8" {
		t.Fatalf("fingerprint = %q", got)
	}
}

func writePreparedTerminal(t *testing.T, root, id string, status Status) (string, []byte) {
	t.Helper()
	dir := writeStoredRun(t, root, storedRunFixture{
		id: id, name: "payments", path: "/repos/payments", status: StatusRunning,
		started: "2026-08-27T10:00:00Z",
	})
	raw := canonicalRaw(t)
	run := raw["run"].(map[string]any)
	run["id"] = id
	run["status"] = string(status)
	run["completed_at"] = "2026-08-27T10:02:03Z"
	if status == StatusFailed {
		run["error"] = "fixture failed"
	}
	prepared, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, preparedBaselineName), prepared)
	return dir, prepared
}

func writeTerminalLog(t *testing.T, dir string, status Status, fingerprint string, newline bool) {
	t.Helper()
	line := "2026-08-27T10:02:03Z status=" + string(status) +
		" baseline_fingerprint=" + fingerprint
	if newline {
		line += "\n"
	}
	mustWrite(t, filepath.Join(dir, "run.log"), []byte(line))
}
