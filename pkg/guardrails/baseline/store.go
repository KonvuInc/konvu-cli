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
	storeRoot, rootInfo, err := openStoreRoot(s.Root)
	if os.IsNotExist(err) {
		return []RunEntry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect baseline store %s: %w", s.Root, err)
	}
	defer storeRoot.Close()
	directory, err := storeRoot.Open(".")
	if err != nil {
		return nil, fmt.Errorf("open baseline store %s: %w", s.Root, err)
	}
	entries, err := directory.ReadDir(-1)
	_ = directory.Close()
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
		runs = append(runs, loadRunEntry(storeRoot, name, path))
	}
	currentRootInfo, err := regularStoreDirectory(s.Root)
	if err != nil || !os.SameFile(rootInfo, currentRootInfo) {
		return nil, fmt.Errorf("baseline store %s changed while it was being read", s.Root)
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

func loadRunEntry(storeRoot *os.Root, id, dir string) RunEntry {
	if !safeRunID(id) {
		return invalidRun(id, dir, "run directory name must be a safe path component")
	}
	runRoot, directoryInfo, err := openStoredRunRoot(storeRoot, id)
	if err != nil {
		return invalidRun(id, dir, err.Error())
	}
	defer runRoot.Close()
	if err := validateRootRegularFile(runRoot, "run.log"); err != nil {
		return invalidRun(id, dir, fmt.Sprintf("invalid run.log: %v", err))
	}
	baselinePath := filepath.Join(dir, "baseline.json")
	logPath := filepath.Join(dir, "run.log")
	data, err := readRootRegularFile(runRoot, "baseline.json")
	if err != nil {
		return invalidRun(id, dir, fmt.Sprintf("invalid baseline.json: %v", err))
	}
	document, err := Parse(data)
	if err != nil {
		if baselineErr, ok := err.(*Error); ok {
			baselineErr.Path = baselinePath
		}
		return invalidRun(id, dir, err.Error())
	}
	if document.Run.ID != id {
		return invalidRun(
			id,
			dir,
			fmt.Sprintf("baseline run id %q does not match directory name %q", document.Run.ID, id),
		)
	}
	if document.Run.Status == StatusRunning {
		recoveredDocument, err := recoverTerminalSnapshot(runRoot, id)
		if err != nil {
			return invalidRun(id, dir, err.Error())
		}
		if recoveredDocument != nil {
			document = recoveredDocument
		}
	}
	currentDirectoryInfo, err := regularStoredRunDirectory(storeRoot, id)
	if err != nil || !os.SameFile(directoryInfo, currentDirectoryInfo) {
		return invalidRun(id, dir, "run directory changed while it was being read")
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

func openStoreRoot(path string) (*os.Root, os.FileInfo, error) {
	expectedInfo, err := regularStoreDirectory(path)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	directory, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	openedInfo, statErr := directory.Stat()
	_ = directory.Close()
	currentInfo, currentErr := regularStoreDirectory(path)
	if statErr != nil || currentErr != nil || !openedInfo.IsDir() ||
		!os.SameFile(expectedInfo, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		_ = root.Close()
		return nil, nil, fmt.Errorf("baseline store changed while it was being opened")
	}
	return root, openedInfo, nil
}

func regularStoreDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("must be a non-symlinked directory")
	}
	return info, nil
}

func openStoredRunRoot(storeRoot *os.Root, id string) (*os.Root, os.FileInfo, error) {
	expectedInfo, err := regularStoredRunDirectory(storeRoot, id)
	if err != nil {
		return nil, nil, err
	}
	root, err := storeRoot.OpenRoot(id)
	if err != nil {
		return nil, nil, err
	}
	directory, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	openedInfo, statErr := directory.Stat()
	_ = directory.Close()
	currentInfo, currentErr := regularStoredRunDirectory(storeRoot, id)
	if statErr != nil || currentErr != nil || !openedInfo.IsDir() ||
		!os.SameFile(expectedInfo, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		_ = root.Close()
		return nil, nil, fmt.Errorf("run directory changed while it was being opened")
	}
	return root, openedInfo, nil
}

func regularStoredRunDirectory(storeRoot *os.Root, id string) (os.FileInfo, error) {
	info, err := storeRoot.Lstat(id)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("run directory must be a non-symlinked directory")
	}
	return info, nil
}

func openRunRoot(path string) (*os.Root, os.FileInfo, error) {
	expectedInfo, err := regularRunDirectory(path)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, nil, err
	}
	directory, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, nil, err
	}
	openedInfo, statErr := directory.Stat()
	_ = directory.Close()
	currentInfo, currentErr := regularRunDirectory(path)
	if statErr != nil || currentErr != nil || !openedInfo.IsDir() ||
		!os.SameFile(expectedInfo, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		_ = root.Close()
		return nil, nil, fmt.Errorf("run directory changed while it was being opened")
	}
	return root, openedInfo, nil
}

func regularRunDirectory(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("run directory must be a non-symlinked directory")
	}
	return info, nil
}

func validateRootRegularFile(root *os.Root, name string) error {
	file, err := openRootRegularFile(root, name)
	if err != nil {
		return err
	}
	return file.Close()
}

func readRootRegularFile(root *os.Root, name string) ([]byte, error) {
	file, err := openRootRegularFile(root, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readOpenedRootRegularFile(root, name, file)
}

func readOpenedRootRegularFile(root *os.Root, name string, file *os.File) ([]byte, error) {
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if err := validateOpenedRootRegularFile(root, name, file); err != nil {
		return nil, err
	}
	return data, nil
}

func openRootRegularFile(root *os.Root, name string) (*os.File, error) {
	return openRootRegularFileWithOpen(root, name, func() (*os.File, error) {
		return root.Open(name)
	})
}

func openRootRegularFileForUpdate(root *os.Root, name string) (*os.File, error) {
	return openRootRegularFileWithOpen(root, name, func() (*os.File, error) {
		return root.OpenFile(name, os.O_RDWR, 0)
	})
}

func openRootRegularFileWithOpen(
	root *os.Root,
	name string,
	open func() (*os.File, error),
) (*os.File, error) {
	expectedInfo, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if expectedInfo.Mode()&os.ModeSymlink != 0 || !expectedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("must be a regular, non-symlinked file")
	}
	file, err := open()
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	currentInfo, currentErr := root.Lstat(name)
	if currentErr != nil || currentInfo.Mode()&os.ModeSymlink != 0 ||
		!openedInfo.Mode().IsRegular() || !currentInfo.Mode().IsRegular() ||
		!os.SameFile(expectedInfo, openedInfo) || !os.SameFile(openedInfo, currentInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("regular file changed while it was being opened")
	}
	return file, nil
}

func validateOpenedRootRegularFile(root *os.Root, name string, file *os.File) error {
	openedInfo, err := file.Stat()
	if err != nil {
		return err
	}
	currentInfo, err := root.Lstat(name)
	if err != nil || currentInfo.Mode()&os.ModeSymlink != 0 ||
		!openedInfo.Mode().IsRegular() || !currentInfo.Mode().IsRegular() ||
		!os.SameFile(openedInfo, currentInfo) {
		return fmt.Errorf("regular file changed while it was being read")
	}
	return nil
}

func syncOpenedRootRegularFile(root *os.Root, name string, file *os.File) error {
	if err := validateOpenedRootRegularFile(root, name, file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return validateOpenedRootRegularFile(root, name, file)
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
