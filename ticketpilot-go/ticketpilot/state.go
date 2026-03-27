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
	RepoOwner     string     `json:"repo_owner"`
	RepoName      string     `json:"repo_name"`
	IssueNumber   int        `json:"issue_number"`
	SessionID     string     `json:"session_id,omitempty"`
	LastRepliedAt *time.Time `json:"last_replied_at,omitempty"`
}

type state struct {
	StartedAtTime *time.Time            `json:"started_at,omitempty"`
	Tickets       map[string]TicketInfo `json:"tickets"`

	path string
}

// LoadState reads or creates state from the given path.
func LoadState(path string) (StateStore, error) {
	s := &state{
		path:    path,
		Tickets: make(map[string]TicketInfo),
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

	if s.Tickets == nil {
		s.Tickets = make(map[string]TicketInfo)
	}

	return s, nil
}

func (s *state) StartedAt() *time.Time { return s.StartedAtTime }

func (s *state) SetStartedAt(t time.Time) {
	t2 := t
	s.StartedAtTime = &t2
}

func (s *state) SessionFor(ticketID string) string {
	return s.Tickets[ticketID].SessionID
}

func (s *state) SetSession(ticketID, sessionID string) {
	t := s.Tickets[ticketID]
	t.SessionID = sessionID
	s.Tickets[ticketID] = t
}

func (s *state) LastRepliedAt(ticketID string) *time.Time {
	return s.Tickets[ticketID].LastRepliedAt
}

func (s *state) SetLastRepliedAt(ticketID string, t time.Time) {
	ti := s.Tickets[ticketID]
	t2 := t
	ti.LastRepliedAt = &t2
	s.Tickets[ticketID] = ti
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
