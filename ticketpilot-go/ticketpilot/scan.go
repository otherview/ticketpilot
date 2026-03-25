package ticketpilot

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// commentsAfter returns the slice of comments that follow anchorID in thread.
// If anchorID is empty, the full thread is returned (no anchor recorded yet).
// If anchorID is not found, nil is returned (anchor is outside the lookback
// window; the session already has the context).
func commentsAfter(thread []Comment, anchorID string) []Comment {
	if anchorID == "" {
		return thread
	}
	for i, c := range thread {
		if c.ID == anchorID {
			return thread[i+1:]
		}
	}
	return nil
}

// Scan finds the next unprocessed @handle mention in the GitHub Project.
// Returns a ScanResult with Pending=false when nothing is waiting.
// When a mention is found, records the ticket location in state and saves.
func (tp *TicketPilot) Scan(ctx context.Context) (*ScanResult, error) {
	since := time.Now().UTC().AddDate(0, 0, -tp.cfg.LookbackDays)
	if last := tp.st.GetLastRunAt(); last != nil {
		since = *last
	}

	tp.log.Info("scan started", "since", since)

	mention, err := tp.gh.GetNextMention(ctx, since, tp.st.IsProcessed, tp.st.SessionFor)
	if err != nil {
		return nil, fmt.Errorf("getting next mention: %w", err)
	}

	if mention == nil {
		tp.log.Info("scan complete", "pending", false)
		tp.st.SetLastRunAt(time.Now().UTC())
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

	// No prior session: keep the full thread so the caller has context to start
	// a new conversation. With an existing session: return only the delta —
	// comments that arrived after the last comment the bot replied to.
	if mention.SessionID != nil {
		anchorID := tp.st.LastProcessedComment(mention.TicketID)
		mention.Thread = commentsAfter(mention.Thread, anchorID)
	}

	tp.st.RecordTicket(mention.TicketID, mention.RepoOwner, mention.RepoName, mention.IssueNumber)
	if err := tp.st.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return &ScanResult{Pending: true, Mention: mention}, nil
}
