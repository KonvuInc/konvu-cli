package cmd

import (
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/KonvuInc/konvu-cli/pkg/api"
	clierrors "github.com/KonvuInc/konvu-cli/pkg/errors"
	"github.com/KonvuInc/konvu-cli/pkg/output"
	"github.com/spf13/cobra"
)

const (
	knowledgeUploadsPath = "/knowledge/uploads"
	// The server's limits: 20 MiB a file, 20 files a request.
	knowledgeUploadMaxBytes = 20 << 20
	knowledgeUploadBatch    = 20
	// The most uploads the server lists in one answer.
	knowledgeUploadsMaxLimit = 200
)

// The statuses of a file Konvu stored and reads, or has read. Any other, an unknown one included,
// is a file that did not go through.
var knowledgeUploadStored = map[string]bool{"pending": true, "processing": true, "ready": true}

var knowledgeUploadCmd = &cobra.Command{
	Use: "upload <file>...", Args: cobra.MinimumNArgs(1), SilenceUsage: true,
	Short: "Send documents to Konvu for your Security Book",
	Long: `Send documents, such as architecture notes or security policies, to Konvu.
Konvu reads them to fix and extend your company's Security Book.

A file must be at most 20 MiB. A directory, an empty or unreadable file, or one
over the limit is refused here and never sent. Files go 20 to a request.

The table shows each file's status: pending or processing while Konvu reads it,
ready once read, failed or unsupported when Konvu could not read it. Run
'konvu knowledge uploads' to follow them.

Exit codes: 0 every file was sent and none failed, 1 a file was refused, failed
or unsupported (the error names each), 2 invalid arguments, 3 a server that does
not take uploads, 4 not logged in, a read-only key or a demo view.`,
	Example: "  konvu knowledge upload architecture.pdf security-policy.docx\n  konvu knowledge upload notes/*.md -o json",
	RunE:    runKnowledgeUpload,
}

var knowledgeUploadsCmd = &cobra.Command{
	Use: "uploads", Args: cobra.NoArgs, SilenceUsage: true,
	Short: "List the documents sent to Konvu for your Security Book",
	Long: `List the documents sent to Konvu for your company's Security Book, newest
first, with their status: pending or processing while Konvu reads them, ready
once read, failed or unsupported when Konvu could not read them.

Exit codes: 0 listed, 1 an error, 2 invalid arguments, 3 a server that does not
take uploads, 4 not logged in or no access.`,
	Example: "  konvu knowledge uploads\n  konvu knowledge uploads --limit 5 -o json\n  konvu knowledge uploads --offset 50",
	RunE:    runKnowledgeUploads,
}

func runKnowledgeUpload(cmd *cobra.Command, args []string) error {
	explicit, _ := cmd.Flags().GetString("output")
	format, err := vulnerabilityReportOutputFormat(explicit)
	if err != nil {
		return err
	}
	// The table names a file as Konvu stores it, by its base name, which FormatTable rarely has to
	// cut; the error names each file that did not go through as it was typed.
	rows := make([]any, len(args))
	failures := []any{}
	var problems []string
	problem := func(index int, reason string) {
		problems = append(problems, fmt.Sprintf("%s (%s)", sanitizeGuardrailsBaselineText(args[index]), reason))
	}
	refuse := func(index int, status, reason string) {
		row := rows[index].(map[string]any)
		row["status"], row["error"] = status, reason
		failures = append(failures, map[string]any{"file": args[index], "status": status, "error": reason})
		problem(index, reason)
	}
	var send []int
	for index, name := range args {
		rows[index] = map[string]any{"file": sanitizeGuardrailsBaselineText(filepath.Base(name)), "size": "", "status": "", "error": ""}
		info, err := os.Stat(name)
		reason := "cannot be read"
		if err == nil {
			reason = knowledgeUploadRefusal(info)
			if info.Mode().IsRegular() {
				rows[index].(map[string]any)["size"] = knowledgeSize(float64(info.Size()))
			}
		}
		if reason != "" {
			refuse(index, "refused", reason)
			continue
		}
		send = append(send, index)
	}

	items := []any{}
	var requestErr error
	if len(send) > 0 {
		client := api.NewClient("", "")
		defer client.Close()
		for start := 0; start < len(send); start += knowledgeUploadBatch {
			batch := send[start:min(start+knowledgeUploadBatch, len(send))]
			if requestErr != nil {
				for _, index := range batch {
					refuse(index, "not sent", "an earlier request failed")
				}
				continue
			}
			sent, result, err := postKnowledgeUploads(client, args, batch, refuse)
			if err != nil {
				requestErr = err
				status, reason := knowledgeUploadRequestFailure(err)
				for _, index := range sent {
					refuse(index, status, reason)
				}
				continue
			}
			answered := getSlice(result, "items")
			// Items come in the order sent, but the server leaves out an upload deleted since it was
			// stored: with one missing, no item can be matched to its file.
			if len(answered) != len(sent) {
				items = append(items, answered...)
				for _, index := range sent {
					refuse(index, "unknown", "the server's answer does not match the files sent: check 'konvu knowledge uploads'")
				}
				continue
			}
			for position, index := range sent {
				item, _ := answered[position].(map[string]any)
				items = append(items, item)
				status := getStr(item, "status")
				row := rows[index].(map[string]any)
				row["status"] = orDefault(sanitizeGuardrailsBaselineText(status), "unknown")
				row["error"] = sanitizeGuardrailsBaselineText(getStr(item, "error"))
				if size, ok := getFloat(item, "size_bytes"); ok {
					row["size"] = knowledgeSize(size)
				}
				if !knowledgeUploadStored[status] {
					problem(index, row["status"].(string))
				}
			}
		}
	}

	if format == output.JSON {
		err = output.WriteString(cmd.OutOrStdout(), output.FormatJSON(map[string]any{"items": items, "errors": failures})+"\n")
	} else {
		err = output.WriteString(cmd.OutOrStdout(), output.FormatTable(map[string]any{"files": rows}, []string{"file", "size", "status", "error"}, "files", nil)+"\n")
	}
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("%d of %d files did not go through: %s", len(problems), len(args), strings.Join(problems, ", "))
	if requestErr != nil {
		mapped := knowledgeWriteError(requestErr, "")
		if cliErr, ok := mapped.(*clierrors.CLIError); ok {
			cliErr.Message += "; " + summary
		}
		return mapped
	}
	if len(problems) > 0 {
		return &clierrors.CLIError{
			Code:       "UPLOAD_INCOMPLETE",
			Message:    summary,
			Suggestion: "Fix or convert the files named here, then upload only those again.",
			ExitCode:   clierrors.ExitGeneralError,
		}
	}
	return nil
}

// postKnowledgeUploads sends one request with the files of batch it can open, refusing the others,
// and returns the files it sent in the order sent.
func postKnowledgeUploads(client *api.Client, args []string, batch []int, refuse func(int, string, string)) ([]int, map[string]any, error) {
	var sent []int
	var parts []api.MultipartFile
	for _, index := range batch {
		file, err := os.Open(args[index])
		if err != nil {
			refuse(index, "refused", "cannot be read")
			continue
		}
		defer file.Close()
		// Measured again on the open file: what was checked is what is sent.
		info, err := file.Stat()
		reason := "cannot be read"
		if err == nil {
			reason = knowledgeUploadRefusal(info)
		}
		if reason != "" {
			refuse(index, "refused", reason)
			continue
		}
		contentType := mime.TypeByExtension(filepath.Ext(args[index]))
		parts = append(parts, api.MultipartFile{Field: "file", Filename: filepath.Base(args[index]), ContentType: contentType, Size: info.Size(), Body: file})
		sent = append(sent, index)
	}
	if len(parts) == 0 {
		return nil, map[string]any{}, nil
	}
	result, err := client.PostMultipart(knowledgeUploadsPath, parts)
	return sent, result, err
}

// knowledgeUploadRequestFailure is what became of the files of a failed request. A refusal stored
// nothing; after a server error or a lost connection the server may have stored them.
func knowledgeUploadRequestFailure(err error) (string, string) {
	var apiErr *api.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.StatusCode < 500, errors.As(err, new(*api.AuthenticationError)), errors.As(err, new(*api.RedirectError)):
		return "refused", "the server refused the request"
	}
	return "unknown", "the request failed: check 'konvu knowledge uploads' before sending it again"
}

// knowledgeUploadRefusal says why a file can't be sent, or "" when it can.
func knowledgeUploadRefusal(info os.FileInfo) string {
	switch {
	case info.IsDir():
		return "a directory"
	case !info.Mode().IsRegular():
		return "not a regular file"
	case info.Size() > knowledgeUploadMaxBytes:
		return "over 20 MiB"
	case info.Size() == 0:
		return "empty"
	}
	return ""
}

func knowledgeSize(bytes float64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MiB", bytes/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KiB", bytes/(1<<10))
	}
	return fmt.Sprintf("%.0f B", bytes)
}

func runKnowledgeUploads(cmd *cobra.Command, args []string) error {
	explicit, _ := cmd.Flags().GetString("output")
	format, err := vulnerabilityReportOutputFormat(explicit)
	if err != nil {
		return err
	}
	params := map[string]any{}
	if cmd.Flags().Changed("limit") {
		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 1 || limit > knowledgeUploadsMaxLimit {
			return knowledgeUsage("--limit must be from 1 to 200", "Pass how many uploads to list, newest first; --offset skips the newest ones.")
		}
		params["limit"] = limit
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		return knowledgeUsage("--offset cannot be negative", "Pass how many of the newest uploads to skip.")
	}
	if offset > 0 {
		params["offset"] = offset
	}
	client := api.NewClient("", "")
	defer client.Close()
	result, err := client.Get(knowledgeUploadsPath, params)
	if err != nil {
		if apiErr, ok := err.(*api.APIError); ok {
			switch apiErr.StatusCode {
			case 404, 405:
				return knowledgeUploadsUnavailable()
			case 422:
				return knowledgeUsage(invariantDetail(apiErr), "Pass --limit from 1 to 200 and a positive --offset, or leave them out.")
			}
		}
		return knowledgePagesError(err, "")
	}
	if format == output.JSON {
		return output.WriteString(cmd.OutOrStdout(), output.FormatJSON(result)+"\n")
	}
	items := getSlice(result, "items")
	total, hasTotal := getFloat(result, "total")
	if len(items) == 0 {
		if offset > 0 && hasTotal {
			return output.WriteString(cmd.OutOrStdout(), fmt.Sprintf("No uploads past the newest %d; there are %.0f.\n", offset, total))
		}
		return output.WriteString(cmd.OutOrStdout(), "No uploads yet. Send documents with 'konvu knowledge upload <files>'.\n")
	}
	rows := make([]any, 0, len(items))
	for _, item := range items {
		upload, _ := item.(map[string]any)
		size := ""
		if value, ok := getFloat(upload, "size_bytes"); ok {
			size = knowledgeSize(value)
		}
		uploaded := getStr(upload, "created_at")
		if len(uploaded) >= len("2006-01-02") {
			uploaded = uploaded[:len("2006-01-02")]
		}
		rows = append(rows, map[string]any{
			"file":        sanitizeGuardrailsBaselineText(getStr(upload, "filename")),
			"status":      sanitizeGuardrailsBaselineText(getStr(upload, "status")),
			"size":        size,
			"uploaded_by": sanitizeGuardrailsBaselineText(getStr(upload, "uploaded_by")),
			"uploaded":    sanitizeGuardrailsBaselineText(uploaded),
			"error":       sanitizeGuardrailsBaselineText(getStr(upload, "error")),
		})
	}
	text := output.FormatTable(map[string]any{"uploads": rows}, []string{"file", "status", "size", "uploaded_by", "uploaded", "error"}, "uploads", nil) + "\n"
	if hasTotal && int(total) > offset+len(items) {
		text += fmt.Sprintf("Showing uploads %d to %d of %.0f, newest first. Pass --offset %d to see the next ones.\n", offset+1, offset+len(items), total, offset+len(items))
	}
	return output.WriteString(cmd.OutOrStdout(), text)
}

func knowledgeUploadsUnavailable() error {
	return &clierrors.CLIError{
		Code: "KNOWLEDGE_UPLOADS_UNAVAILABLE", Message: "this Konvu server does not take Security Book uploads",
		Suggestion: "Check KONVU_API_URL, or try again once your Konvu server is up to date.",
		ExitCode:   clierrors.ExitNotFound,
	}
}

func init() {
	knowledgeCmd.AddCommand(knowledgeUploadCmd, knowledgeUploadsCmd)
	knowledgeUploadCmd.Flags().StringP("output", "o", "", "Output format: json or table")
	knowledgeUploadsCmd.Flags().StringP("output", "o", "", "Output format: json or table")
	knowledgeUploadsCmd.Flags().Int("limit", 0, "How many uploads to list, from 1 to 200 (default: the server's, 50)")
	knowledgeUploadsCmd.Flags().Int("offset", 0, "How many of the newest uploads to skip")
}
