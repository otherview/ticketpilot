package tests

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/otherview/ticketpilot/ticketpilot"
)

// newTestLogger returns a debug-level slog.Logger that writes through t.Log
// when the test is run with -v, and nil (silent) otherwise.
func newTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	if !testing.Verbose() {
		return nil
	}
	return slog.New(slog.NewTextHandler(&tWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// tWriter adapts testing.T into an io.Writer so slog can route through t.Log.
type tWriter struct{ t *testing.T }

func (w *tWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// --- FakeGitHubClient ---

// FakeGitHubClient implements ticketpilot.GitHubClient for testing.
// It iterates Mentions in order, skipping processed ones and populating
// SessionID from state — mirroring the real client's behaviour.
type FakeGitHubClient struct {
	Mentions       []*ticketpilot.Mention
	PostedComments []PostedComment
	Err            error // returned by GetNextMention when set
}

type PostedComment struct {
	RepoOwner   string
	RepoName    string
	IssueNumber int
	Body        string
}

func (f *FakeGitHubClient) GetNextMention(
	_ context.Context,
	_ time.Time,
	isProcessed func(string) bool,
	sessionFor func(string) string,
) (*ticketpilot.Mention, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	for _, m := range f.Mentions {
		if isProcessed(m.CommentID) {
			continue
		}
		out := *m // shallow copy — don't mutate the original
		if s := sessionFor(m.TicketID); s != "" {
			out.SessionID = &s
		} else {
			out.SessionID = nil
		}
		return &out, nil
	}
	return nil, nil
}

func (f *FakeGitHubClient) PostComment(_ context.Context, repoOwner, repoName string, issueNumber int, body string) error {
	f.PostedComments = append(f.PostedComments, PostedComment{repoOwner, repoName, issueNumber, body})
	return nil
}

// --- FakeStateStore ---

// FakeStateStore implements ticketpilot.StateStore in memory.
type FakeStateStore struct {
	processed map[string]bool
	tickets   map[string]fakeTicket
	lastRunAt *time.Time
	SaveCalls int
}

type fakeTicket struct {
	repoOwner              string
	repoName               string
	issueNumber            int
	sessionID              string
	lastProcessedCommentID string
}

func newFakeState() *FakeStateStore {
	return &FakeStateStore{
		processed: make(map[string]bool),
		tickets:   make(map[string]fakeTicket),
	}
}

func (s *FakeStateStore) IsProcessed(id string) bool       { return s.processed[id] }
func (s *FakeStateStore) MarkProcessed(id string)           { s.processed[id] = true }
func (s *FakeStateStore) SessionFor(ticketID string) string { return s.tickets[ticketID].sessionID }

func (s *FakeStateStore) SetSession(ticketID, sessionID string) {
	t := s.tickets[ticketID]
	t.sessionID = sessionID
	s.tickets[ticketID] = t
}

func (s *FakeStateStore) RecordTicket(ticketID, owner, repo string, num int) {
	t := s.tickets[ticketID] // preserve existing sessionID
	t.repoOwner = owner
	t.repoName = repo
	t.issueNumber = num
	s.tickets[ticketID] = t
}

func (s *FakeStateStore) TicketLocation(ticketID string) (string, string, int, bool) {
	t, ok := s.tickets[ticketID]
	if !ok || t.repoOwner == "" {
		return "", "", 0, false
	}
	return t.repoOwner, t.repoName, t.issueNumber, true
}

func (s *FakeStateStore) LastProcessedComment(ticketID string) string {
	return s.tickets[ticketID].lastProcessedCommentID
}

func (s *FakeStateStore) SetLastProcessedComment(ticketID, commentID string) {
	t := s.tickets[ticketID]
	t.lastProcessedCommentID = commentID
	s.tickets[ticketID] = t
}

func (s *FakeStateStore) GetLastRunAt() *time.Time { return s.lastRunAt }
func (s *FakeStateStore) SetLastRunAt(t time.Time) { s.lastRunAt = &t }
func (s *FakeStateStore) Save() error              { s.SaveCalls++; return nil }
