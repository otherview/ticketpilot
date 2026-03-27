package ticketpilot

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Scan finds the next pending @handle mention in the GitHub Project.
// On the very first call (started_at not set) it initialises the start anchor
// and returns Pending=false without scanning — this prevents the bot from
// replying to historical mentions on startup.
// Subsequent calls use cutoffFor(ticketID) = max(started_at, last_replied_at)
// to only look at new activity since the last interaction.
func (tp *TicketPilot) Scan(ctx context.Context) (*ScanResult, error) {
	if tp.st.StartedAt() == nil {
		now := time.Now().UTC()
		tp.st.SetStartedAt(now)
		if err := tp.st.Save(); err != nil {
			return nil, fmt.Errorf("saving state: %w", err)
		}
		tp.log.Info("first run: initialized started_at, skipping historical mentions")
		return &ScanResult{Pending: false}, nil
	}

	cutoffFor := func(ticketID string) time.Time {
		startedAt := *tp.st.StartedAt()
		if lr := tp.st.LastRepliedAt(ticketID); lr != nil && lr.After(startedAt) {
			return *lr
		}
		return startedAt
	}

	tp.log.Info("scan started")

	mention, err := tp.gh.GetNextMention(ctx, cutoffFor, tp.st.SessionFor)
	if err != nil {
		return nil, fmt.Errorf("getting next mention: %w", err)
	}

	if mention == nil {
		tp.log.Info("scan complete", "pending", false)
		if err := tp.st.Save(); err != nil {
			return nil, fmt.Errorf("saving state: %w", err)
		}
		return &ScanResult{Pending: false}, nil
	}

	tp.log.Info("mention found",
		"ticket_id", mention.TicketID,
		"comment_id", mention.CommentID,
	)
	tp.log.Debug("mention detail",
		"title", mention.Title,
		"type", mention.Type,
		"comment_author", mention.CommentAuthor,
		"has_session", mention.SessionID != nil,
		slog.Int("thread_length", len(mention.Thread)),
	)

	tp.st.RecordTicket(mention.TicketID, mention.RepoOwner, mention.RepoName, mention.IssueNumber)
	if err := tp.st.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return &ScanResult{Pending: true, Mention: mention}, nil
}
