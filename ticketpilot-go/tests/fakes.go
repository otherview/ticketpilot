package tests

import (
	"context"
	"fmt"
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
// It uses the same time-based cutoff algorithm as the real client:
// filter comments to those after cutoff, walk newest-first to find the
// last @handle mention, build a chronological thread up to that point.
type FakeGitHubClient struct {
	Handle         string // e.g. "bot"
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
	cutoffFor func(string) time.Time,
	sessionFor func(string) string,
) (*ticketpilot.Mention, error) {
	if f.Err != nil {
		return nil, f.Err
	}

	handle := strings.ToLower(f.Handle)

	for _, m := range f.Mentions {
		cutoff := cutoffFor(m.TicketID)

		// Filter to comments after cutoff and reverse to get newest-first.
		commentsDesc := filterDesc(m.Thread, cutoff)

		// Find first non-bot @handle mention in desc order = last chronological mention.
		mentionIdx := -1
		for i, c := range commentsDesc {
			if !strings.EqualFold(c.Author, handle) &&
				strings.Contains(strings.ToLower(c.Body), "@"+handle) {
				mentionIdx = i
				break
			}
		}
		if mentionIdx < 0 {
			continue
		}

		// Build chronological thread up to and including the mention.
		thread := reverseComments(commentsDesc[mentionIdx:])

		s := sessionFor(m.TicketID)
		var sessionID *string
		if s != "" {
			sessionID = &s
		}

		mc := commentsDesc[mentionIdx]
		out := *m
		out.CommentID = mc.ID
		out.CommentAuthor = mc.Author
		out.CommentBody = mc.Body
		out.Thread = thread
		out.SessionID = sessionID
		return &out, nil
	}
	return nil, nil
}

func (f *FakeGitHubClient) PostComment(_ context.Context, repoOwner, repoName string, issueNumber int, body string) error {
	f.PostedComments = append(f.PostedComments, PostedComment{repoOwner, repoName, issueNumber, body})
	// Append the bot reply to the matching mention's thread so subsequent
	// scans see it and can detect "bot replied".
	ticketID := fmt.Sprintf("%s/%s#%d", repoOwner, repoName, issueNumber)
	for _, m := range f.Mentions {
		if m.TicketID == ticketID {
			m.Thread = append(m.Thread, ticketpilot.Comment{
				ID:        fmt.Sprintf("bot-reply-%d", len(f.PostedComments)),
				Author:    f.Handle,
				Body:      body,
				CreatedAt: time.Now(),
			})
			break
		}
	}
	return nil
}

// filterDesc returns comments with CreatedAt strictly after cutoff, in
// reverse-chronological order (newest first).
func filterDesc(thread []ticketpilot.Comment, cutoff time.Time) []ticketpilot.Comment {
	var filtered []ticketpilot.Comment
	for _, c := range thread {
		if c.CreatedAt.After(cutoff) {
			filtered = append(filtered, c)
		}
	}
	// reverse to newest-first
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered
}

// reverseComments returns a new slice with the comments in reverse order.
func reverseComments(c []ticketpilot.Comment) []ticketpilot.Comment {
	out := make([]ticketpilot.Comment, len(c))
	for i, v := range c {
		out[len(c)-1-i] = v
	}
	return out
}

// --- FakeStateStore ---

// FakeStateStore implements ticketpilot.StateStore in memory.
type FakeStateStore struct {
	tickets     map[string]fakeTicket
	startedAt   *time.Time
	lastReplied map[string]*time.Time
	SaveCalls   int
}

type fakeTicket struct {
	repoOwner   string
	repoName    string
	issueNumber int
	sessionID   string
}

func newFakeState() *FakeStateStore {
	return &FakeStateStore{
		tickets:     make(map[string]fakeTicket),
		lastReplied: make(map[string]*time.Time),
	}
}

func (s *FakeStateStore) StartedAt() *time.Time { return s.startedAt }

func (s *FakeStateStore) SetStartedAt(t time.Time) {
	t2 := t
	s.startedAt = &t2
}

func (s *FakeStateStore) LastRepliedAt(ticketID string) *time.Time { return s.lastReplied[ticketID] }

func (s *FakeStateStore) SetLastRepliedAt(ticketID string, t time.Time) {
	t2 := t
	s.lastReplied[ticketID] = &t2
}

func (s *FakeStateStore) SessionFor(ticketID string) string { return s.tickets[ticketID].sessionID }

func (s *FakeStateStore) SetSession(ticketID, sessionID string) {
	t := s.tickets[ticketID]
	t.sessionID = sessionID
	s.tickets[ticketID] = t
}

func (s *FakeStateStore) RecordTicket(ticketID, owner, repo string, num int) {
	t := s.tickets[ticketID]
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

func (s *FakeStateStore) Save() error { s.SaveCalls++; return nil }
