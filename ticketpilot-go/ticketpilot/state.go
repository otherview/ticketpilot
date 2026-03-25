package ticketpilot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TicketInfo holds the persisted data for a single project ticket.
type TicketInfo struct {
	RepoOwner              string `json:"repo_owner"`
	RepoName               string `json:"repo_name"`
	IssueNumber            int    `json:"issue_number"`
	SessionID              string `json:"session_id,omitempty"`
	LastProcessedCommentID string `json:"last_processed_comment_id,omitempty"`
}

type state struct {
	ProcessedComments []string              `json:"processed_comments"`
	Tickets           map[string]TicketInfo `json:"tickets"`
	LastRunAt         *time.Time            `json:"last_run_at,omitempty"`

	path      string
	processed map[string]struct{} // fast O(1) lookup
}

// LoadState reads or creates state from the given path.
func LoadState(path string) (StateStore, error) {
	s := &state{
		path:      path,
		Tickets:   make(map[string]TicketInfo),
		processed: make(map[string]struct{}),
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state: %w", err)
	}

	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("parsing state: %w", err)
	}

	for _, id := range s.ProcessedComments {
		s.processed[id] = struct{}{}
	}
	if s.Tickets == nil {
		s.Tickets = make(map[string]TicketInfo)
	}

	return s, nil
}

func (s *state) IsProcessed(commentID string) bool {
	_, ok := s.processed[commentID]
	return ok
}

func (s *state) MarkProcessed(commentID string) {
	if _, ok := s.processed[commentID]; !ok {
		s.processed[commentID] = struct{}{}
		s.ProcessedComments = append(s.ProcessedComments, commentID)
	}
}

func (s *state) SessionFor(ticketID string) string {
	return s.Tickets[ticketID].SessionID
}

func (s *state) SetSession(ticketID, sessionID string) {
	t := s.Tickets[ticketID]
	t.SessionID = sessionID
	s.Tickets[ticketID] = t
}

func (s *state) LastProcessedComment(ticketID string) string {
	return s.Tickets[ticketID].LastProcessedCommentID
}

func (s *state) SetLastProcessedComment(ticketID, commentID string) {
	t := s.Tickets[ticketID]
	t.LastProcessedCommentID = commentID
	s.Tickets[ticketID] = t
}

func (s *state) RecordTicket(ticketID, repoOwner, repoName string, issueNumber int) {
	t := s.Tickets[ticketID]
	t.RepoOwner = repoOwner
	t.RepoName = repoName
	t.IssueNumber = issueNumber
	s.Tickets[ticketID] = t
}

func (s *state) TicketLocation(ticketID string) (repoOwner, repoName string, issueNumber int, ok bool) {
	t, exists := s.Tickets[ticketID]
	if !exists || t.RepoOwner == "" {
		return "", "", 0, false
	}
	return t.RepoOwner, t.RepoName, t.IssueNumber, true
}

func (s *state) GetLastRunAt() *time.Time {
	return s.LastRunAt
}

func (s *state) SetLastRunAt(t time.Time) {
	s.LastRunAt = &t
}

// Save writes state atomically: write to .tmp then rename, so a crash
// mid-write never corrupts the state file.
func (s *state) Save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating state dir: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("writing state: %w", err)
	}

	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	return nil
}
