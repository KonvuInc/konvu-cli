package baseline

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

const preparedBaselineName = ".baseline.json.tmp"

type terminalRecord struct {
	status      Status
	fingerprint string
}

func recoverTerminalSnapshot(root *os.Root, id string) (*Document, error) {
	return recoverTerminalSnapshotWithOperations(
		root,
		id,
		syncOpenedRootRegularFile,
		root.Rename,
	)
}

func recoverTerminalSnapshotWithRename(
	root *os.Root,
	id string,
	rename func(string, string) error,
) (*Document, error) {
	return recoverTerminalSnapshotWithOperations(root, id, syncOpenedRootRegularFile, rename)
}

func recoverTerminalSnapshotWithOperations(
	root *os.Root,
	id string,
	syncLog func(*os.Root, string, *os.File) error,
	rename func(string, string) error,
) (*Document, error) {
	logFile, err := openRootRegularFileForUpdate(root, "run.log")
	if err != nil {
		return nil, recoveryError("opening run.log for recovery: %v", err)
	}
	defer logFile.Close()
	log, err := readOpenedRootRegularFile(root, "run.log", logFile)
	if err != nil {
		return nil, recoveryError("reading run.log: %v", err)
	}
	record, err := parseTerminalRecord(log)
	if err != nil || record == nil {
		return nil, err
	}
	info, err := root.Lstat(preparedBaselineName)
	if errors.Is(err, os.ErrNotExist) {
		if installed := installedSnapshot(root, id, record); installed != nil {
			if err := syncLog(root, "run.log", logFile); err != nil {
				return nil, recoveryError("syncing run.log: %v", err)
			}
			if installed = installedSnapshot(root, id, record); installed == nil {
				return nil, recoveryError("installed baseline changed during recovery")
			}
			if err := syncRootDirectory(root); err != nil {
				return nil, recoveryError("syncing baseline directory: %v", err)
			}
			return installed, nil
		}
		return nil, recoveryError("terminal log has no prepared baseline snapshot")
	}
	if err != nil {
		return nil, recoveryError("reading prepared baseline: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, recoveryError("prepared baseline is not a regular, non-symlinked file")
	}
	prepared, err := readRootRegularFile(root, preparedBaselineName)
	if err != nil {
		return nil, recoveryError("reading prepared baseline: %v", err)
	}
	if _, err := validatePreparedSnapshot(prepared, id, record); err != nil {
		return nil, err
	}
	if err := syncLog(root, "run.log", logFile); err != nil {
		return nil, recoveryError("syncing run.log: %v", err)
	}
	if err := rename(preparedBaselineName, "baseline.json"); err != nil {
		installed := installedSnapshot(root, id, record)
		if installed == nil {
			return nil, recoveryError("installing prepared baseline: %v", err)
		}
		if removeErr := root.Remove(preparedBaselineName); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, recoveryError("removing redundant prepared baseline: %v", removeErr)
		}
		if installed = installedSnapshot(root, id, record); installed == nil {
			return nil, recoveryError("installed baseline changed during recovery")
		}
		if syncErr := syncRootDirectory(root); syncErr != nil {
			return nil, recoveryError("syncing baseline directory: %v", syncErr)
		}
		return installed, nil
	}
	installed := installedSnapshot(root, id, record)
	if installed == nil {
		return nil, recoveryError("installed baseline does not match the terminal log")
	}
	if err := syncRootDirectory(root); err != nil {
		return nil, recoveryError("syncing baseline directory: %v", err)
	}
	return installed, nil
}

func parseTerminalRecord(log []byte) (*terminalRecord, error) {
	if len(log) == 0 || log[len(log)-1] != '\n' {
		return nil, nil
	}
	if !utf8.Valid(log) {
		return nil, recoveryError("run.log is not valid UTF-8")
	}
	lines := strings.Split(strings.TrimSuffix(string(log), "\n"), "\n")
	if len(lines) == 0 {
		return nil, nil
	}
	fields := make(map[string]string)
	for _, field := range strings.Fields(lines[len(lines)-1]) {
		if name, value, ok := strings.Cut(field, "="); ok {
			if _, found := fields[name]; !found {
				fields[name] = value
			}
		}
	}
	status := Status(fields["status"])
	if status != StatusCompleted && status != StatusFailed && status != StatusCancelled {
		return nil, nil
	}
	fingerprint := fields["baseline_fingerprint"]
	if fingerprint == "" {
		return nil, recoveryError("terminal run.log record has no baseline fingerprint")
	}
	return &terminalRecord{status: status, fingerprint: fingerprint}, nil
}

func validatePreparedSnapshot(data []byte, id string, record *terminalRecord) (*Document, error) {
	document, err := Parse(data)
	if err != nil {
		return nil, recoveryError("invalid prepared baseline: %v", err)
	}
	if document.Run.ID != id {
		return nil, recoveryError("prepared baseline run id does not match directory")
	}
	if document.Run.Status != record.status {
		return nil, recoveryError("prepared baseline status does not match run.log")
	}
	if baselineFingerprint(data) != record.fingerprint {
		return nil, recoveryError("prepared baseline fingerprint does not match run.log")
	}
	return document, nil
}

func installedSnapshot(root *os.Root, id string, record *terminalRecord) *Document {
	data, err := readRootRegularFile(root, "baseline.json")
	if err != nil {
		return nil
	}
	document, err := validatePreparedSnapshot(data, id, record)
	if err != nil {
		return nil
	}
	return document
}

func baselineFingerprint(data []byte) string {
	const prime uint64 = 0x00000100000001b3
	hash := uint64(0xcbf29ce484222325)
	for _, value := range data {
		hash ^= uint64(value)
		hash *= prime
	}
	return fmt.Sprintf("fnv1a64:%016x", hash)
}

func recoveryError(format string, values ...any) error {
	return fmt.Errorf("baseline recovery pending: "+format, values...)
}
