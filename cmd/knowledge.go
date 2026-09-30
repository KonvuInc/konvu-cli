package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

var knowledgeCmd = &cobra.Command{
	Use: "knowledge", Short: "Manage knowledge used during triage",
}

var triageInstructionsCmd = newTriageInstructionsCommand()

func newTriageInstructionsCommand() *cobra.Command {
	parent := &cobra.Command{
		Use: "triage-instructions", SilenceUsage: true, Short: "Manage repository triage instruction files",
		Long: "Manage Markdown instructions that apply at the next triage. Existing verdicts are unchanged.",
	}
	for _, operation := range []string{"list", "get", "upload", "delete"} {
		operation := operation
		command := &cobra.Command{
			Use: operation, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error { return runTriageInstructions(cmd, args, operation) },
		}
		switch operation {
		case "list":
			command.Short = "List instruction files for a repository"
		case "get":
			command.Use, command.Args = "get <file-id>", cobra.ExactArgs(1)
			command.Short = "Read an instruction file and its revision"
		case "upload":
			command.Short = "Upload a Markdown instruction file"
			command.Long = `Create an instruction file from --file, or stdin with --file -.

Use --file-id and --base-sha256 to replace a file against the revision returned
by get. A stale revision is refused. --path defaults to the local filename;
stdin requires --path. Use a repository-relative Markdown path such as /scope.md.`
			command.Flags().String("file", "", "Markdown file to upload; '-' reads stdin")
			command.Flags().String("path", "", "Stored Markdown path (defaults to the local filename)")
			command.Flags().String("file-id", "", "Existing file ID to replace")
			command.Flags().String("base-sha256", "", "Revision from get; required when replacing a file")
			command.Example = "  konvu knowledge triage-instructions upload --repo github:acme/web --file scope.md"
		case "delete":
			command.Use, command.Args = "delete <file-id>", cobra.ExactArgs(1)
			command.Short = "Delete a file against a known revision"
			command.Flags().String("base-sha256", "", "Revision from get (required)")
		}
		command.Flags().String("repo", "", "Repository, such as github:acme/web (required)")
		command.Flags().StringP("output", "o", "", "Output format: json or table")
		parent.AddCommand(command)
	}
	return parent
}

func instructionUsage(message, suggestion string) error {
	return &clierrors.CLIError{
		Code: "INVALID_ARGUMENTS", Message: message, Suggestion: suggestion, ExitCode: clierrors.ExitUsageError,
	}
}

func instructionFileID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, "/\\?#%") || id == "." || id == ".." {
		return "", instructionUsage("invalid file ID", "Use the file ID returned by list or get.")
	}
	return id, nil
}

func instructionRepository(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"github:", "https://github.com/", "http://github.com/", "git@github.com:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return strings.TrimSuffix(strings.TrimRight(value, "/"), ".git")
}

func instructionTarget(client *api.Client, repo string) (string, error) {
	response, err := client.Get(vulnerabilityReportsBase+"/programs", nil)
	if err != nil {
		return "", instructionAPIError(err)
	}
	wanted := instructionRepository(repo)
	var matches []string
	for _, item := range getSlice(response, "items") {
		row, _ := item.(map[string]any)
		repositories := getSlice(row, "source_code_repos")
		if len(repositories) == 0 && getStr(row, "repository") != "" {
			repositories = []any{getStr(row, "repository")}
		}
		for _, repository := range repositories {
			name, _ := repository.(string)
			if name != "" && instructionRepository(name) == wanted {
				id, err := instructionFileID(getStr(row, "id"))
				if err != nil {
					return "", instructionAPIError(fmt.Errorf("invalid repository target"))
				}
				matches = append(matches, id)
				break
			}
		}
	}
	if len(matches) != 1 {
		return "", &clierrors.CLIError{
			Code: "REPOSITORY_NOT_RESOLVED", Message: "repository must match exactly one triage target",
			Suggestion: "Check the repository's triage configuration in Konvu, then retry with its full repository name.",
			ExitCode:   clierrors.ExitUsageError,
		}
	}
	return matches[0], nil
}

func readInstructions(cmd *cobra.Command, file string) (string, error) {
	var reader io.Reader = cmd.InOrStdin()
	if file != "-" {
		opened, err := os.Open(file)
		if err != nil {
			return "", instructionUsage("cannot read instruction file", "Pass a readable Markdown file, or --file - for stdin.")
		}
		defer opened.Close()
		reader = opened
	}
	const maxBytes = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil || len(data) > maxBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", instructionUsage("instruction file must be UTF-8 text under 1 MiB", "Upload a Markdown text file.")
	}
	content := strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n"))
	if content == "" {
		return "", instructionUsage("instruction file is empty", "Add instructions before uploading the file.")
	}
	return content, nil
}

func runTriageInstructions(cmd *cobra.Command, args []string, operation string) error {
	repo, _ := cmd.Flags().GetString("repo")
	if strings.TrimSpace(repo) == "" {
		return instructionUsage("--repo is required", "Pass the full repository name, such as github:acme/web.")
	}
	explicit, _ := cmd.Flags().GetString("output")
	format, err := vulnerabilityReportOutputFormat(explicit)
	if err != nil {
		return err
	}
	var id, revision string
	if len(args) > 0 {
		id, err = instructionFileID(args[0])
		if err != nil {
			return err
		}
	}
	if cmd.Flags().Lookup("base-sha256") != nil {
		revision, _ = cmd.Flags().GetString("base-sha256")
		revision = strings.TrimSpace(revision)
	}
	var payload map[string]any
	if operation == "upload" {
		file, _ := cmd.Flags().GetString("file")
		path, _ := cmd.Flags().GetString("path")
		id, _ = cmd.Flags().GetString("file-id")
		id = strings.TrimSpace(id)
		if id != "" {
			id, err = instructionFileID(id)
			if err != nil {
				return err
			}
		}
		if file == "" || (id == "") != (revision == "") {
			return instructionUsage("--file is required; --file-id and --base-sha256 must be used together", "Use upload --help for examples.")
		}
		path = strings.TrimSpace(path)
		if path == "" && file != "-" {
			path = filepath.Base(file)
		}
		if path == "" || !strings.HasSuffix(path, ".md") {
			return instructionUsage("--path must name a Markdown file", "Pass --path /scope.md; stdin requires an explicit path.")
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		content, err := readInstructions(cmd, file)
		if err != nil {
			return err
		}
		payload = map[string]any{"path": path, "content": content, "client": "cli"}
		if id != "" {
			payload["base_sha256"] = revision
		}
	}
	if operation == "delete" && revision == "" {
		return instructionUsage("--base-sha256 is required", "Read the file with get and pass its sha256 to delete.")
	}
	client := api.NewClient("", "")
	defer client.Close()
	target, err := instructionTarget(client, repo)
	if err != nil {
		return err
	}
	path := vulnerabilityReportsBase + "/programs/" + target + "/policy/files"
	if id != "" {
		path += "/" + id
	}
	var result map[string]any
	switch operation {
	case "list", "get":
		result, err = client.Get(path, nil)
	case "upload":
		if id == "" {
			result, err = client.Post(path, payload)
		} else {
			result, err = client.Put(path, payload)
		}
	case "delete":
		_, err = client.Delete(path, map[string]any{"base_sha256": revision, "client": "cli"})
		result = map[string]any{"id": id, "deleted": true}
	}
	if err != nil {
		return instructionAPIError(err)
	}
	if operation == "upload" {
		delete(result, "content")
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(result)+"\n")
	}
	if operation == "get" {
		metadata := output.FormatTable(map[string]any{"files": []any{result}}, []string{"id", "path", "sha256"}, "files", nil)
		return output.WriteString(cmd.OutOrStdout(), metadata+"\n\n"+getStr(result, "content")+"\n")
	}
	if operation == "list" {
		return output.WriteString(cmd.OutOrStdout(), output.FormatTable(result, []string{"id", "path", "size", "sha256"}, "files", nil)+"\n")
	}
	return output.WriteString(cmd.OutOrStdout(), output.FormatTable(map[string]any{"files": []any{result}}, []string{"id", "path", "sha256", "deleted"}, "files", nil)+"\n")
}

func instructionAPIError(err error) error {
	if _, ok := err.(*api.AuthenticationError); ok {
		return clierrors.NewAuthError(err.Error())
	}
	suggestion := "Check repository access and the instruction file, then retry."
	code := "INSTRUCTIONS_REQUEST_FAILED"
	exitCode := clierrors.ExitGeneralError
	if apiErr, ok := err.(*api.APIError); ok {
		switch apiErr.StatusCode {
		case 403:
			suggestion = "Check that your credentials have access to this repository and write access for uploads or deletes."
		case 404:
			exitCode = clierrors.ExitNotFound
			suggestion = "List the repository's instruction files to check its current file IDs."
		case 409:
			code = "INSTRUCTIONS_CONFLICT"
			suggestion = "Read the current file with get, review its content and sha256, then retry. New files need a unique path."
		case 422:
			suggestion = "Use a non-empty Markdown text file and a valid path; check the limits returned by list."
		}
	}
	return &clierrors.CLIError{Code: code, Message: "could not complete triage instructions request", Suggestion: suggestion, ExitCode: exitCode}
}

func init() {
	knowledgeCmd.AddCommand(triageInstructionsCmd)
	rootCmd.AddCommand(knowledgeCmd)
}
