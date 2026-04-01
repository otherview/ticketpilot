package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/otherview/ticketpilot/ticketpilot"
)

// --- JSON output types ---

type scanOutput struct {
	Pending       bool            `json:"pending"`
	TicketID      string          `json:"ticket_id,omitempty"`
	CommentID     string          `json:"comment_id,omitempty"`
	Title         string          `json:"title,omitempty"`
	Type          string          `json:"type,omitempty"`
	RepoOwner     string          `json:"repo_owner,omitempty"`
	RepoName      string          `json:"repo_name,omitempty"`
	IssueNumber   int             `json:"issue_number,omitempty"`
	CommentAuthor string          `json:"comment_author,omitempty"`
	CommentBody   string          `json:"comment_body,omitempty"`
	CommentThread []threadComment `json:"comment_thread,omitempty"`
	SessionID     *string         `json:"session_id"` // explicitly null when no prior session
}

type threadComment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type replyOutput struct {
	Success   bool   `json:"success"`
	TicketID  string `json:"ticket_id"`
	CommentID string `json:"comment_id"`
	SessionID string `json:"session_id"`
}

type createOutput struct {
	Success       bool   `json:"success"`
	TicketID      string `json:"ticket_id"`
	RepoOwner     string `json:"repo_owner"`
	RepoName      string `json:"repo_name"`
	IssueNumber   int    `json:"issue_number"`
	IssueURL      string `json:"issue_url"`
	ProjectItemID int64  `json:"project_item_id"`
}

// --- root command ---

var (
	verbose bool
	envFile string
)

var rootCmd = &cobra.Command{
	Use:   "ticketpilot",
	Short: "GitHub Project mention scanner and reply poster",
}

// newLogger returns an slog.Logger writing to stderr.
// When verbose is true the level is Debug, otherwise Info.
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// --- scan ---

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Find the next unprocessed @handle mention in the GitHub Project",
	RunE:  runScan,
}

func runScan(cmd *cobra.Command, _ []string) error {
	cfg, err := ticketpilot.LoadConfig(envFile)
	if err != nil {
		return err
	}

	st, err := ticketpilot.LoadState(cfg.StateFile)
	if err != nil {
		return err
	}

	gh := ticketpilot.NewGitHubClient(cfg, newLogger())
	tp := ticketpilot.New(gh, st, cfg, newLogger())

	result, err := tp.Scan(cmd.Context())
	if err != nil {
		return fmt.Errorf("scanning project: %w", err)
	}

	if !result.Pending {
		return writeJSON(scanOutput{Pending: false})
	}

	m := result.Mention
	thread := make([]threadComment, len(m.Thread))
	for i, c := range m.Thread {
		thread[i] = threadComment{Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt}
	}

	return writeJSON(scanOutput{
		Pending:       true,
		TicketID:      m.TicketID,
		CommentID:     m.CommentID,
		Title:         m.Title,
		Type:          m.Type,
		RepoOwner:     m.RepoOwner,
		RepoName:      m.RepoName,
		IssueNumber:   m.IssueNumber,
		CommentAuthor: m.CommentAuthor,
		CommentBody:   m.CommentBody,
		CommentThread: thread,
		SessionID:     m.SessionID,
	})
}

// --- reply ---

var (
	replyTicketID  string
	replyCommentID string
	replySessionID string
	replyBody      string
)

var replyCmd = &cobra.Command{
	Use:   "reply",
	Short: "Post a reply to the pending mention",
	RunE:  runReply,
}

var (
	createRepoOwner string
	createRepoName  string
	createTitle     string
	createBody      string
)

var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an issue and add it to the configured GitHub Project",
	RunE:  runCreate,
}

func runReply(cmd *cobra.Command, _ []string) error {
	cfg, err := ticketpilot.LoadConfig(envFile)
	if err != nil {
		return err
	}

	st, err := ticketpilot.LoadState(cfg.StateFile)
	if err != nil {
		return err
	}

	// Body comes from --body flag or stdin.
	body := strings.TrimSpace(replyBody)
	if body == "" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("reading body from stdin: %w", err)
		}
		body = strings.TrimSpace(string(raw))
	}
	if body == "" {
		return fmt.Errorf("reply body is required: use --body or pipe via stdin")
	}

	gh := ticketpilot.NewGitHubClient(cfg, newLogger())
	tp := ticketpilot.New(gh, st, cfg, newLogger())

	result, err := tp.Reply(cmd.Context(), replyTicketID, replyCommentID, replySessionID, body)
	if err != nil {
		return err
	}

	return writeJSON(replyOutput{
		Success:   true,
		TicketID:  result.TicketID,
		CommentID: result.CommentID,
		SessionID: result.SessionID,
	})
}

func runCreate(cmd *cobra.Command, _ []string) error {
	cfg, err := ticketpilot.LoadConfig(envFile)
	if err != nil {
		return err
	}

	st, err := ticketpilot.LoadState(cfg.StateFile)
	if err != nil {
		return err
	}

	gh := ticketpilot.NewGitHubClient(cfg, newLogger())
	tp := ticketpilot.New(gh, st, cfg, newLogger())

	result, err := tp.Create(cmd.Context(), createRepoOwner, createRepoName, createTitle, createBody)
	if err != nil {
		return err
	}

	return writeJSON(createOutput{
		Success:       true,
		TicketID:      result.TicketID,
		RepoOwner:     result.RepoOwner,
		RepoName:      result.RepoName,
		IssueNumber:   result.IssueNumber,
		IssueURL:      result.IssueURL,
		ProjectItemID: result.ProjectItemID,
	})
}

// --- helpers ---

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func main() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug-level logging to stderr")
	rootCmd.PersistentFlags().StringVar(&envFile, "env-file", "", "Path to .env file (default: .env in current directory)")

	replyCmd.Flags().StringVar(&replyTicketID, "ticket-id", "", "Ticket ID returned by scan (required)")
	replyCmd.Flags().StringVar(&replyCommentID, "comment-id", "", "Comment ID returned by scan (required)")
	replyCmd.Flags().StringVar(&replySessionID, "session-id", "", "Session/conversation ID from the wrapper (required)")
	replyCmd.Flags().StringVar(&replyBody, "body", "", "Reply text (reads from stdin if omitted)")
	_ = replyCmd.MarkFlagRequired("ticket-id")
	_ = replyCmd.MarkFlagRequired("comment-id")
	_ = replyCmd.MarkFlagRequired("session-id")

	createCmd.Flags().StringVar(&createRepoOwner, "repo-owner", "", "Repository owner for the new issue (required)")
	createCmd.Flags().StringVar(&createRepoName, "repo-name", "", "Repository name for the new issue (required)")
	createCmd.Flags().StringVar(&createTitle, "title", "", "Issue title (required)")
	createCmd.Flags().StringVar(&createBody, "body", "", "Issue body (optional)")
	_ = createCmd.MarkFlagRequired("repo-owner")
	_ = createCmd.MarkFlagRequired("repo-name")
	_ = createCmd.MarkFlagRequired("title")

	rootCmd.AddCommand(scanCmd, replyCmd, createCmd)

	if err := rootCmd.ExecuteContext(context.Background()); err != nil {
		os.Exit(1)
	}
}
