package ticketpilot

import (
	"context"
	"io"
	"log/slog"
	"time"

	gh "github.com/google/go-github/v84/github"
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
	GetNextMention(ctx context.Context, cutoffFor func(string) time.Time, sessionFor func(string) string) (*Mention, error)
	PostComment(ctx context.Context, repoOwner, repoName string, issueNumber int, body string) error
	CreateIssue(ctx context.Context, title, body string) (issueNum int, issueID int64, err error)
	ListProjects(ctx context.Context, org string) ([]*gh.ProjectV2, error)
	ListProjectFields(ctx context.Context, org string, projectNumber int) ([]*gh.ProjectV2Field, error)
	AddProjectItem(ctx context.Context, org string, projectNumber int, issueNum int64) (*gh.ProjectV2Item, error)
	UpdateProjectItem(ctx context.Context, org string, projectNumber int, itemID int64, fields []*gh.UpdateProjectV2Field) error
}

// StateStore is the interface for persisting TicketPilot state.
type StateStore interface {
	StartedAt() *time.Time
	SetStartedAt(t time.Time)
	SessionFor(ticketID string) string
	SetSession(ticketID, sessionID string)
	LastRepliedAt(ticketID string) *time.Time
	SetLastRepliedAt(ticketID string, t time.Time)
	RecordTicket(ticketID, repoOwner, repoName string, issueNumber int)
	TicketLocation(ticketID string) (repoOwner, repoName string, issueNumber int, ok bool)
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
