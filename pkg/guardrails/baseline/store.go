package baseline

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// StoreRelativePath is the single machine-wide baseline catalog under HOME.
const StoreRelativePath = ".konvu/guardrails/baselines"

// Store enumerates immutable run directories below Root.
type Store struct {
	Root string
}

// Selector chooses a run without consulting the current working directory.
// RunID takes precedence over Repository.
type Selector struct {
	RunID      string
	Repository string
}

// RunEntry is one run directory. Invalid entries retain their directory ID and
// diagnostic while valid entries expose parsed metadata and their Document.
type RunEntry struct {
	ID       string
	Dir      string
	LogPath  string
	Valid    bool
	Problem  string
	Run      RunMetadata
	Codebase CodebaseMetadata
	Counts   Counts
	Document *Document
}

// DefaultStoreRoot resolves the store from HOME without inspecting cwd.
func DefaultStoreRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determine home directory: %w", err)
	}
	return filepath.Join(home, filepath.FromSlash(StoreRelativePath)), nil
}

// DefaultStore returns the machine-wide baseline store.
func DefaultStore() (Store, error) {
	root, err := DefaultStoreRoot()
	if err != nil {
		return Store{}, err
	}
	return Store{Root: root}, nil
}

// List enumerates every immediate run directory. A broken run becomes one
// invalid result and never prevents other runs from being returned.
func (s Store) List() ([]RunEntry, error) {
	if strings.TrimSpace(s.Root) == "" {
		return nil, fmt.Errorf("baseline store root is empty")
	}
	rootInfo, err := os.Lstat(s.Root)
	if os.IsNotExist(err) {
		return []RunEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect baseline store %s: %w", s.Root, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("baseline store %s must be a non-symlinked directory", s.Root)
	}
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		return nil, fmt.Errorf("read baseline store %s: %w", s.Root, err)
	}

	runs := make([]RunEntry, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(s.Root, name)
		if entry.Type()&os.ModeSymlink != 0 {
			runs = append(runs, invalidRun(name, path, "run directory must not be a symlink"))
			continue
		}
		if !entry.IsDir() {
			continue
		}
		runs = append(runs, loadRunEntry(name, path))
	}
	sort.SliceStable(runs, func(i, j int) bool {
		left, right := runSortTime(runs[i]), runSortTime(runs[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return runs[i].ID < runs[j].ID
	})
	return runs, nil
}

// Select resolves an exact run ID, exact stored absolute path, or unambiguous
// codebase name. Repository selection returns the latest completed run only.
func (s Store) Select(selector Selector) (*RunEntry, error) {
	runs, err := s.List()
	if err != nil {
		return nil, err
	}
	if selector.RunID != "" {
		if !safeRunID(selector.RunID) {
			return nil, &Error{Code: ErrorRunNotFound, Message: fmt.Sprintf("run %q was not found", selector.RunID)}
		}
		for index := range runs {
			if runs[index].ID != selector.RunID {
				continue
			}
			if !runs[index].Valid {
				return nil, &Error{
					Code:    ErrorInvalidArtifact,
					Path:    runs[index].Dir,
					Message: runs[index].Problem,
				}
			}
			return &runs[index], nil
		}
		return nil, &Error{Code: ErrorRunNotFound, Message: fmt.Sprintf("run %q was not found", selector.RunID)}
	}

	repository := strings.TrimSpace(selector.Repository)
	if repository == "" {
		completed := completedRuns(runs)
		if len(completed) == 1 {
			return completed[0], nil
		}
		if len(completed) == 0 {
			return nil, &Error{Code: ErrorRunNotFound, Message: "no completed baseline runs were found"}
		}
		return nil, &Error{
			Code:    ErrorRunAmbiguous,
			Message: "more than one completed baseline run exists; select one by run ID or repository",
		}
	}

	completed := completedRuns(runs)
	if filepath.IsAbs(repository) {
		path := filepath.Clean(repository)
		matches := filterRuns(completed, func(run *RunEntry) bool {
			return run.Codebase.Path == path
		})
		return latestRepositoryRun(matches, repository)
	}

	matches := filterRuns(completed, func(run *RunEntry) bool {
		return run.Codebase.Name == repository
	})
	if len(matches) == 0 {
		return nil, &Error{
			Code:    ErrorRunNotFound,
			Message: fmt.Sprintf("no completed baseline was found for repository %q", repository),
		}
	}
	paths := make(map[string]bool)
	for _, run := range matches {
		paths[run.Codebase.Path] = true
	}
	if len(paths) > 1 {
		return nil, &Error{
			Code:    ErrorRunAmbiguous,
			Message: fmt.Sprintf("repository name %q matches more than one stored path", repository),
		}
	}
	return latestRepositoryRun(matches, repository)
}

func loadRunEntry(id, dir string) RunEntry {
	baselinePath := filepath.Join(dir, "baseline.json")
	logPath := filepath.Join(dir, "run.log")
	if err := regularFile(logPath); err != nil {
		return invalidRun(id, dir, fmt.Sprintf("invalid run.log: %v", err))
	}
	document, err := Load(baselinePath)
	if err != nil {
		return invalidRun(id, dir, err.Error())
	}
	if document.Run.ID != id {
		return invalidRun(
			id,
			dir,
			fmt.Sprintf("baseline run id %q does not match directory name %q", document.Run.ID, id),
		)
	}
	return RunEntry{
		ID:       id,
		Dir:      dir,
		LogPath:  logPath,
		Valid:    true,
		Run:      document.Run,
		Codebase: document.Codebase,
		Counts:   document.Counts,
		Document: document,
	}
}

func invalidRun(id, dir, problem string) RunEntry {
	return RunEntry{
		ID:      id,
		Dir:     dir,
		LogPath: filepath.Join(dir, "run.log"),
		Problem: problem,
	}
}

func regularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("must be a regular, non-symlinked file")
	}
	return nil
}

func readRegularFile(path string) ([]byte, error) {
	return readRegularFileWithOpen(path, os.Open)
}

func readRegularFileWithOpen(
	path string,
	open func(string) (*os.File, error),
) ([]byte, error) {
	file, err := openRegularFile(path, open)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	currentInfo, err := os.Lstat(path)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 ||
		!currentInfo.Mode().IsRegular() || !os.SameFile(openedInfo, currentInfo) {
		return nil, fmt.Errorf("regular file changed while it was being read")
	}
	return data, nil
}

func openRegularFile(
	path string,
	open func(string) (*os.File, error),
) (*os.File, error) {
	expectedInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if expectedInfo.Mode()&os.ModeSymlink != 0 || !expectedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("must be a regular, non-symlinked file")
	}
	file, err := open(path)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	currentInfo, currentErr := os.Lstat(path)
	if currentErr != nil || currentInfo.Mode()&os.ModeSymlink != 0 ||
		!openedInfo.Mode().IsRegular() || !currentInfo.Mode().IsRegular() ||
		!os.SameFile(expectedInfo, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("regular file changed while it was being opened")
	}
	return file, nil
}

func safeRunID(id string) bool {
	return id != "" && id != "." && id != ".." && filepath.Base(id) == id &&
		!strings.ContainsAny(id, `/\\`)
}

func runSortTime(run RunEntry) time.Time {
	for _, value := range []string{run.Run.CompletedAt, run.Run.StartedAt} {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func completedRuns(runs []RunEntry) []*RunEntry {
	result := make([]*RunEntry, 0, len(runs))
	for index := range runs {
		if runs[index].Valid && runs[index].Run.Status == StatusCompleted {
			result = append(result, &runs[index])
		}
	}
	return result
}

func filterRuns(runs []*RunEntry, keep func(*RunEntry) bool) []*RunEntry {
	result := make([]*RunEntry, 0, len(runs))
	for _, run := range runs {
		if keep(run) {
			result = append(result, run)
		}
	}
	return result
}

func latestRepositoryRun(runs []*RunEntry, repository string) (*RunEntry, error) {
	if len(runs) == 0 {
		return nil, &Error{
			Code:    ErrorRunNotFound,
			Message: fmt.Sprintf("no completed baseline was found for repository %q", repository),
		}
	}
	sort.SliceStable(runs, func(i, j int) bool {
		left, right := runSortTime(*runs[i]), runSortTime(*runs[j])
		if !left.Equal(right) {
			return left.After(right)
		}
		return runs[i].ID > runs[j].ID
	})
	return runs[0], nil
}
