package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
)

type guardrailsSandboxPaths struct {
	readOnly []string
	writable []string
	workDir  string
	repoRoot string
}

func sandboxedGuardrailsCommand(
	binPath string,
	args []string,
	env []string,
) (*exec.Cmd, func(), error) {
	paths, childEnv, cleanup, err := prepareGuardrailsSandbox(binPath, args, env)
	if err != nil {
		return nil, func() {}, err
	}
	child, err := platformGuardrailsSandboxCommand(
		binPath,
		guardrailsSandboxArguments(args, paths.repoRoot),
		paths,
	)
	if err != nil {
		cleanup()
		return nil, func() {}, err
	}
	child.Env = childEnv
	return child, cleanup, nil
}

func prepareGuardrailsSandbox(
	binPath string,
	args []string,
	env []string,
) (guardrailsSandboxPaths, []string, func(), error) {
	workDir, err := os.Getwd()
	if err != nil {
		return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
	}

	tempDir, err := os.MkdirTemp("", "konvu-guardrails-sandbox-*")
	if err != nil {
		return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	canonicalWorkDir, err := canonicalPath(workDir)
	if err != nil {
		cleanup()
		return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
	}
	paths := guardrailsSandboxPaths{workDir: canonicalWorkDir}
	if err := paths.addReadOnly(filepath.Dir(binPath)); err != nil {
		cleanup()
		return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
	}
	if err := paths.addWritable(tempDir); err != nil {
		cleanup()
		return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
	}

	repo := guardrailsRepoArgument(args)
	if repo != "" {
		storeRoot, err := prepareGuardrailsStoreRoot(env)
		if err != nil {
			cleanup()
			return guardrailsSandboxPaths{}, nil, func() {}, err
		}
		if err := paths.addWritable(storeRoot); err != nil {
			cleanup()
			return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
		}
		root, err := canonicalPathFrom(workDir, repo)
		if err != nil {
			cleanup()
			return guardrailsSandboxPaths{}, nil, func() {}, sandboxSetupError(err)
		}
		paths.workDir = root
		paths.repoRoot = root
		paths.addCanonicalReadOnly(root)
		paths.addGitPaths(root, env)
		if environmentValue(env, "OPENAI_API_KEY") == "" {
			home := environmentValue(env, "HOME")
			credentials := filepath.Join(home, ".config", "guardrails", "credentials")
			if _, err := os.Stat(credentials); err == nil {
				_ = paths.addReadOnly(credentials)
			}
		}
	}
	for _, name := range []string{"SSL_CERT_DIR", "SSL_CERT_FILE", "CURL_CA_BUNDLE"} {
		if path := environmentValue(env, name); path != "" {
			if _, err := os.Stat(path); err == nil {
				_ = paths.addReadOnly(path)
			}
		}
	}
	for _, tool := range []string{"curl", "git", "rg", "sh"} {
		if path, err := exec.LookPath(tool); err == nil {
			_ = paths.addReadOnly(path)
		}
	}

	childEnv := replaceEnvironmentValues(env, map[string]string{
		"TEMP":   tempDir,
		"TMP":    tempDir,
		"TMPDIR": tempDir,
	})
	return paths, childEnv, cleanup, nil
}

func guardrailsSandboxArguments(args []string, repoRoot string) []string {
	result := append([]string(nil), args...)
	if repoRoot != "" && len(result) >= 3 && result[0] == "baseline" && result[1] == "scan" {
		result[2] = repoRoot
	}
	return result
}

func guardrailsHome(env []string) (string, error) {
	home := environmentValue(env, "HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", sandboxSetupError(err)
		}
	}
	return home, nil
}

func guardrailsRepoArgument(args []string) string {
	if len(args) >= 3 && args[0] == "baseline" && args[1] == "scan" {
		return args[2]
	}
	return ""
}

func prepareGuardrailsStoreRoot(env []string) (string, error) {
	home, err := guardrailsHome(env)
	if err != nil {
		return "", sandboxSetupError(err)
	}
	root, err := canonicalPath(home)
	if err != nil {
		return "", sandboxSetupError(err)
	}
	current := home
	for _, name := range []string{".konvu", "guardrails"} {
		current = filepath.Join(current, name)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return "", err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("refusing non-directory or symlinked baseline store path %s", current)
		}
		canonical, err := canonicalPath(current)
		if err != nil {
			return "", err
		}
		if !pathWithin(root, canonical) {
			return "", fmt.Errorf("baseline store path %s resolves outside home %s", current, root)
		}
	}
	return canonicalPath(current)
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(relative) {
		return false
	}
	if relative == "." {
		return true
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (paths *guardrailsSandboxPaths) addReadOnly(path string) error {
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	paths.addCanonicalReadOnly(canonical)
	return nil
}

func (paths *guardrailsSandboxPaths) addWritable(path string) error {
	canonical, err := canonicalPath(path)
	if err != nil {
		return err
	}
	paths.writable = appendUnique(paths.writable, canonical)
	return nil
}

func (paths *guardrailsSandboxPaths) addCanonicalReadOnly(path string) {
	paths.readOnly = appendUnique(paths.readOnly, path)
}

func (paths *guardrailsSandboxPaths) addGitPaths(root string, env []string) {
	git, err := exec.LookPath("git")
	if err != nil {
		return
	}
	command := exec.Command(git, "rev-parse", "--git-dir", "--git-common-dir")
	command.Dir = root
	command.Env = env
	output, err := command.Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		path := line
		if !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		_ = paths.addReadOnly(path)
	}
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func canonicalPathFrom(workDir, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	return canonicalPath(path)
}

func appendUnique(paths []string, path string) []string {
	for _, existing := range paths {
		if existing == path {
			return paths
		}
	}
	return append(paths, path)
}

func environmentValue(env []string, name string) string {
	prefix := name + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

func replaceEnvironmentValues(env []string, values map[string]string) []string {
	replaced := make([]string, 0, len(env)+len(values))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if _, replace := values[name]; replace {
			continue
		}
		replaced = append(replaced, entry)
	}
	for _, name := range []string{"TEMP", "TMP", "TMPDIR"} {
		if value, ok := values[name]; ok {
			replaced = append(replaced, name+"="+value)
		}
	}
	return replaced
}

func sandboxSetupError(err error) error {
	return &clierrors.CLIError{
		Code:       "SANDBOX_ERROR",
		Message:    fmt.Sprintf("could not configure the Guardrails sandbox: %v", err),
		Suggestion: "Check path permissions or rerun with --no-sandbox.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}

func sandboxUnavailableError(err error) error {
	return &clierrors.CLIError{
		Code:       "SANDBOX_UNAVAILABLE",
		Message:    fmt.Sprintf("could not start the Guardrails sandbox: %v", err),
		Suggestion: "Install the required OS sandbox or rerun with --no-sandbox.",
		ExitCode:   clierrors.ExitGeneralError,
	}
}
