package baseline

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestStoreListKeepsInvalidRunsWithoutHidingValidRuns(t *testing.T) {
	root := t.TempDir()
	writeStoredRun(t, root, storedRunFixture{
		id:        "api--abc12345--000001",
		name:      "api",
		path:      "/repos/acme/api",
		status:    StatusCompleted,
		started:   "2026-08-27T10:00:00Z",
		completed: "2026-08-27T10:00:12Z",
	})

	corrupt := filepath.Join(root, "corrupt--abc12345--000002")
	mustMkdir(t, corrupt)
	mustWrite(t, filepath.Join(corrupt, "baseline.json"), []byte("{"))
	mustWrite(t, filepath.Join(corrupt, "run.log"), []byte("failed\n"))

	missing := filepath.Join(root, "missing--abc12345--000003")
	mustMkdir(t, missing)
	mustWrite(t, filepath.Join(missing, "run.log"), []byte("failed\n"))

	noLog := filepath.Join(root, "no-log--abc12345--000004")
	mustMkdir(t, noLog)
	raw := canonicalRaw(t)
	raw["run"].(map[string]any)["id"] = filepath.Base(noLog)
	mustWriteJSON(t, filepath.Join(noLog, "baseline.json"), raw)

	mismatch := filepath.Join(root, "mismatch--abc12345--000005")
	mustMkdir(t, mismatch)
	mustWriteJSON(t, filepath.Join(mismatch, "baseline.json"), canonicalRaw(t))
	mustWrite(t, filepath.Join(mismatch, "run.log"), []byte("done\n"))

	mustWrite(t, filepath.Join(root, "README.txt"), []byte("ignored"))

	runs, err := (Store{Root: root}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 5 {
		t.Fatalf("runs = %d, want 5: %#v", len(runs), runs)
	}
	valid := 0
	problems := make(map[string]string)
	for _, run := range runs {
		if run.Valid {
			valid++
			if run.Run.Status != StatusCompleted || run.Counts.Assets != 4 || run.Document == nil {
				t.Fatalf("valid run summary = %#v", run)
			}
		} else {
			problems[run.ID] = run.Problem
		}
	}
	if valid != 1 {
		t.Fatalf("valid runs = %d, want 1", valid)
	}
	for id, fragment := range map[string]string{
		"corrupt--abc12345--000002":  "invalid JSON",
		"missing--abc12345--000003":  "baseline.json",
		"no-log--abc12345--000004":   "run.log",
		"mismatch--abc12345--000005": "does not match directory name",
	} {
		if !strings.Contains(problems[id], fragment) {
			t.Errorf("problem for %s = %q, want %q", id, problems[id], fragment)
		}
	}
}

func TestStoreListMarksSymlinkedRunsAndArtifactsInvalid(t *testing.T) {
	root := t.TempDir()
	targetRoot := t.TempDir()
	target := writeStoredRun(t, targetRoot, storedRunFixture{
		id:        "target--abc12345--000001",
		name:      "target",
		path:      "/repos/target",
		status:    StatusCompleted,
		started:   "2026-08-27T10:00:00Z",
		completed: "2026-08-27T10:00:12Z",
	})
	if err := os.Symlink(target, filepath.Join(root, "linked-run--abc12345--000002")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	linkedArtifact := filepath.Join(root, "linked-artifact--abc12345--000003")
	mustMkdir(t, linkedArtifact)
	if err := os.Symlink(
		filepath.Join(target, "baseline.json"),
		filepath.Join(linkedArtifact, "baseline.json"),
	); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(linkedArtifact, "run.log"), []byte("done\n"))

	runs, err := (Store{Root: root}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Valid || runs[1].Valid {
		t.Fatalf("symlink entries = %#v", runs)
	}
	joined := runs[0].Problem + " " + runs[1].Problem
	if !strings.Contains(joined, "symlink") && !strings.Contains(joined, "non-symlinked") {
		t.Fatalf("symlink diagnostics = %q", joined)
	}
}

func TestStoreListHidesInitializingAndEnforcesRunEnvelope(t *testing.T) {
	root := t.TempDir()
	hidden := ".payments--01234567--000001.initializing"
	mustMkdir(t, filepath.Join(root, hidden))
	hiddenMalformed := ".NOT-A-RUN.initializing"
	mustMkdir(t, filepath.Join(root, hiddenMalformed))

	malformed := "Payments--01234567--000002"
	malformedDir := filepath.Join(root, malformed)
	mustMkdir(t, malformedDir)
	mustWrite(t, filepath.Join(malformedDir, "baseline.json"), canonicalData(t))
	mustWrite(t, filepath.Join(malformedDir, "run.log"), []byte("done\n"))

	extraID := "payments--01234567--000003"
	extraDir := writeStoredRun(t, root, storedRunFixture{
		id: extraID, name: "payments", path: "/repos/payments", status: StatusCompleted,
		started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:12Z",
	})
	mustWrite(t, filepath.Join(extraDir, "extra.json"), []byte("{}"))

	runs, err := (Store{Root: root}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs = %#v, want only two visible invalid rows", runs)
	}
	problems := make(map[string]string, len(runs))
	for _, run := range runs {
		if run.Valid {
			t.Fatalf("run unexpectedly valid: %#v", run)
		}
		problems[run.ID] = run.Problem
	}
	if !strings.Contains(problems[malformed], "must match") {
		t.Fatalf("malformed ID problem = %q", problems[malformed])
	}
	if !strings.Contains(problems[extraID], "unexpected artifact") {
		t.Fatalf("extra artifact problem = %q", problems[extraID])
	}
	if _, visible := problems[hidden]; visible {
		t.Fatalf("initializing directory was visible: %#v", runs)
	}
	if _, visible := problems[hiddenMalformed]; visible {
		t.Fatalf("malformed initializing directory was visible: %#v", runs)
	}
}

func TestStoreRunningRunAllowsOnlyAtomicCheckpointTemp(t *testing.T) {
	root := t.TempDir()
	runID := "payments--01234567--000001"
	directory := writeStoredRun(t, root, storedRunFixture{
		id: runID, name: "payments", path: "/repos/payments", status: StatusRunning,
		started: "2026-08-27T10:00:00Z",
	})
	mustWrite(t, filepath.Join(directory, ".baseline.json.tmp"), []byte("partial checkpoint"))

	runs, err := (Store{Root: root}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || !runs[0].Valid || runs[0].Run.Status != StatusRunning {
		t.Fatalf("running checkpoint run = %#v", runs)
	}

	mustWrite(t, filepath.Join(directory, "unexpected.tmp"), []byte("unexpected"))
	runs, err = (Store{Root: root}).List()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Valid || !strings.Contains(runs[0].Problem, "unexpected.tmp") {
		t.Fatalf("running run with unknown extra = %#v", runs)
	}
}

func TestReadRegularFileRejectsSymlinkSwapBetweenInspectionAndOpen(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "run.log")
	original := filepath.Join(directory, "original.log")
	outside := filepath.Join(directory, "outside.log")
	mustWrite(t, path, []byte("expected\n"))
	mustWrite(t, outside, []byte("secret\n"))

	_, err := readRegularFileWithOpen(path, func(name string) (*os.File, error) {
		if renameErr := os.Rename(name, original); renameErr != nil {
			return nil, renameErr
		}
		if linkErr := os.Symlink(outside, name); linkErr != nil {
			return nil, linkErr
		}
		return os.Open(name)
	})
	if err == nil || !strings.Contains(err.Error(), "changed while it was being opened") {
		t.Fatalf("race error = %v", err)
	}
}

func TestStoreSelectUsesExactRunPathAndUnambiguousName(t *testing.T) {
	root := t.TempDir()
	writeStoredRun(t, root, storedRunFixture{
		id:        "api--aaaaaaaa--000001",
		name:      "api",
		path:      "/repos/acme/api",
		status:    StatusCompleted,
		started:   "2026-08-26T10:00:00Z",
		completed: "2026-08-26T10:00:10Z",
	})
	writeStoredRun(t, root, storedRunFixture{
		id:        "api--bbbbbbbb--000002",
		name:      "api",
		path:      "/repos/acme/api",
		status:    StatusCompleted,
		started:   "2026-08-27T10:00:00Z",
		completed: "2026-08-27T10:00:10Z",
	})
	writeStoredRun(t, root, storedRunFixture{
		id:        "api--cccccccc--000003",
		name:      "api",
		path:      "/repos/acme/api",
		status:    StatusFailed,
		started:   "2026-08-28T10:00:00Z",
		completed: "2026-08-28T10:00:10Z",
	})
	store := Store{Root: root}

	byPath, err := store.Select(Selector{Repository: "/repos/acme/api"})
	if err != nil || byPath.ID != "api--bbbbbbbb--000002" {
		t.Fatalf("path selection = %#v, error = %v", byPath, err)
	}
	byName, err := store.Select(Selector{Repository: "api"})
	if err != nil || byName.ID != "api--bbbbbbbb--000002" {
		t.Fatalf("name selection = %#v, error = %v", byName, err)
	}
	exact, err := store.Select(Selector{
		RunID:      "api--cccccccc--000003",
		Repository: "/does/not/matter",
	})
	if err != nil || exact.Run.Status != StatusFailed {
		t.Fatalf("exact failed run = %#v, error = %v", exact, err)
	}
	if _, err := store.Select(Selector{RunID: "../api--bbbbbbbb--000002"}); errorCode(err) != ErrorRunNotFound {
		t.Fatalf("unsafe id error = %#v", err)
	}
	if _, err := store.Select(Selector{Repository: "missing"}); errorCode(err) != ErrorRunNotFound {
		t.Fatalf("missing repo error = %#v", err)
	}
}

func TestStoreSelectRejectsAmbiguousNameAndImplicitSelection(t *testing.T) {
	root := t.TempDir()
	for _, fixture := range []storedRunFixture{
		{
			id: "api-a--aaaaaaaa--000001", name: "api", path: "/repos/acme/api",
			status: StatusCompleted, started: "2026-08-26T10:00:00Z", completed: "2026-08-26T10:00:10Z",
		},
		{
			id: "api-b--bbbbbbbb--000002", name: "api", path: "/repos/other/api",
			status: StatusCompleted, started: "2026-08-27T10:00:00Z", completed: "2026-08-27T10:00:10Z",
		},
	} {
		writeStoredRun(t, root, fixture)
	}
	store := Store{Root: root}
	if _, err := store.Select(Selector{Repository: "api"}); errorCode(err) != ErrorRunAmbiguous {
		t.Fatalf("ambiguous name error = %#v", err)
	}
	if _, err := store.Select(Selector{}); errorCode(err) != ErrorRunAmbiguous {
		t.Fatalf("implicit selection error = %#v", err)
	}

	secondRoot := t.TempDir()
	writeStoredRun(t, secondRoot, storedRunFixture{
		id: "only--aaaaaaaa--000001", name: "only", path: "/repos/only",
		status: StatusCompleted, started: "2026-08-26T10:00:00Z", completed: "2026-08-26T10:00:10Z",
	})
	only, err := (Store{Root: secondRoot}).Select(Selector{})
	if err != nil || only.ID != "only--aaaaaaaa--000001" {
		t.Fatalf("implicit single selection = %#v, error = %v", only, err)
	}
}

func TestStoreOrdersAndSelectsByStartedAtThenDescendingID(t *testing.T) {
	root := t.TempDir()
	for _, fixture := range []storedRunFixture{
		{
			id: "api--aaaaaaaa--000001", name: "api", path: "/repos/api",
			status: StatusCompleted, started: "2026-08-27T10:00:00Z", completed: "2026-08-27T12:00:00Z",
		},
		{
			id: "api--bbbbbbbb--000002", name: "api", path: "/repos/api",
			status: StatusCompleted, started: "2026-08-27T11:00:00Z", completed: "2026-08-27T11:01:00Z",
		},
		{
			id: "api--cccccccc--000003", name: "api", path: "/repos/api",
			status: StatusCompleted, started: "2026-08-27T11:00:00Z", completed: "2026-08-27T11:01:00Z",
		},
	} {
		writeStoredRun(t, root, fixture)
	}
	store := Store{Root: root}
	runs, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	got := []string{runs[0].ID, runs[1].ID, runs[2].ID}
	want := []string{
		"api--cccccccc--000003",
		"api--bbbbbbbb--000002",
		"api--aaaaaaaa--000001",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	selected, err := store.Select(Selector{Repository: "api"})
	if err != nil || selected.ID != want[0] {
		t.Fatalf("selection = %#v, error = %v", selected, err)
	}
}

func TestStoreNeverInfersRepositoryFromCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	repo := t.TempDir()
	writeStoredRun(t, root, storedRunFixture{
		id: "service--aaaaaaaa--000001", name: "service", path: repo,
		status: StatusCompleted, started: "2026-08-26T10:00:00Z", completed: "2026-08-26T10:00:10Z",
	})
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	if _, err := (Store{Root: root}).Select(Selector{Repository: "."}); errorCode(err) != ErrorRunNotFound {
		t.Fatalf("relative selector unexpectedly used cwd: %v", err)
	}
	selected, err := (Store{Root: root}).Select(Selector{Repository: repo})
	if err != nil || selected.Codebase.Path != repo {
		t.Fatalf("absolute selector = %#v, error = %v", selected, err)
	}
}

func TestStoreMissingAndUnsafeRoots(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	runs, err := (Store{Root: missing}).List()
	if err != nil || len(runs) != 0 {
		t.Fatalf("missing store = %#v, error = %v", runs, err)
	}
	if _, err := (Store{}).List(); err == nil {
		t.Fatal("empty root was accepted")
	}
	if _, err := (Store{Root: "relative"}).List(); err == nil {
		t.Fatal("relative root was accepted")
	}

	realRoot := t.TempDir()
	linkRoot := filepath.Join(t.TempDir(), "store")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := (Store{Root: linkRoot}).List(); err == nil {
		t.Fatal("symlinked store root was accepted")
	}
}

func TestDefaultStoreRootUsesHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root, err := DefaultStoreRoot()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".konvu", "guardrails", "baselines")
	if root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
}

func TestDefaultStoreRootRejectsRelativeHome(t *testing.T) {
	t.Setenv("HOME", "relative-home")
	if _, err := DefaultStoreRoot(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("DefaultStoreRoot error = %v", err)
	}
}

func TestSafeRunIDAcceptsOnlyCanonicalVisibleShape(t *testing.T) {
	for _, id := range []string{
		"my.repo--0123abcd--000001",
		"my_repo--no-commit--999999",
		"my--repo--0123abcd--000001",
	} {
		if !safeRunID(id) {
			t.Errorf("safeRunID(%q) = false, want true", id)
		}
	}
	for _, id := range []string{
		"../repo--0123abcd--000001",
		"Repo--0123abcd--000001",
		"repo--0123ABCD--000001",
		"repo--0123abc--000001",
		"repo--0123abcd--1",
		"repo--0123abcd--000001\n",
	} {
		if safeRunID(id) {
			t.Errorf("safeRunID(%q) = true, want false", id)
		}
	}
}

type storedRunFixture struct {
	id        string
	name      string
	path      string
	status    Status
	started   string
	completed string
}

func writeStoredRun(t *testing.T, root string, fixture storedRunFixture) string {
	t.Helper()
	dir := filepath.Join(root, fixture.id)
	mustMkdir(t, dir)
	raw := canonicalRaw(t)
	run := raw["run"].(map[string]any)
	run["id"] = fixture.id
	run["status"] = string(fixture.status)
	run["started_at"] = fixture.started
	if fixture.completed == "" {
		run["completed_at"] = nil
	} else {
		run["completed_at"] = fixture.completed
	}
	if fixture.status == StatusFailed {
		run["error"] = "fixture failed"
	}
	codebase := raw["codebase"].(map[string]any)
	codebase["name"] = fixture.name
	codebase["path"] = fixture.path
	mustWriteJSON(t, filepath.Join(dir, "baseline.json"), raw)
	mustWrite(t, filepath.Join(dir, "run.log"), []byte("completed\n"))
	return dir
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, data)
}

func mustWrite(t *testing.T, path string, value []byte) {
	t.Helper()
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
}

func errorCode(err error) ErrorCode {
	var baselineErr *Error
	if errors.As(err, &baselineErr) {
		return baselineErr.Code
	}
	return ""
}
