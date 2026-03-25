package ticketpilot

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// Comment is a single comment on a GitHub issue or PR.
type Comment struct {
	ID        string
	Author    string
	Body      string
	CreatedAt time.Time
}

// Mention is an unprocessed @handle comment found in a project item.
type Mention struct {
	TicketID      string
	CommentID     string
	Title         string
	Type          string // "Issue" or "PullRequest"
	RepoOwner     string
	RepoName      string
	IssueNumber   int
	CommentAuthor string
	CommentBody   string
	Thread        []Comment
	SessionID     *string // nil when no prior session exists for this ticket
}

// ScanResult is the output of a Scan call.
type ScanResult struct {
	Pending bool
	Mention *Mention // nil when Pending is false
}

// ReplyResult is the output of a Reply call.
type ReplyResult struct {
	TicketID  string
	CommentID string
	SessionID string
}

// GitHubClient is the interface for GitHub operations.
// Implementations are expected to have project coordinates baked in at construction time.
type GitHubClient interface {
	GetNextMention(ctx context.Context, since time.Time, isProcessed func(string) bool, sessionFor func(string) string) (*Mention, error)
	PostComment(ctx context.Context, repoOwner, repoName string, issueNumber int, body string) error
}

// StateStore is the interface for persisting TicketPilot state.
type StateStore interface {
	IsProcessed(commentID string) bool
	SessionFor(ticketID string) string
	RecordTicket(ticketID, repoOwner, repoName string, issueNumber int)
	TicketLocation(ticketID string) (repoOwner, repoName string, issueNumber int, ok bool)
	MarkProcessed(commentID string)
	SetSession(ticketID, sessionID string)
	LastProcessedComment(ticketID string) string
	SetLastProcessedComment(ticketID, commentID string)
	GetLastRunAt() *time.Time
	SetLastRunAt(t time.Time)
	Save() error
}

// TicketPilot orchestrates scanning for mentions and posting replies.
type TicketPilot struct {
	gh  GitHubClient
	st  StateStore
	cfg *Config
	log *slog.Logger
}

// New creates a TicketPilot with the given dependencies.
// Pass a nil logger to silence all output; otherwise the logger's level
// controls verbosity — Info for key events, Debug for operational detail.
func New(gh GitHubClient, st StateStore, cfg *Config, logger *slog.Logger) *TicketPilot {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &TicketPilot{gh: gh, st: st, cfg: cfg, log: logger}
}
