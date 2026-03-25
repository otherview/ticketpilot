package ticketpilot

import (
	"context"
	"fmt"
)

// Reply posts a reply to the given ticket and marks the comment as processed.
// ticketID must match a ticket previously recorded by Scan.
func (tp *TicketPilot) Reply(ctx context.Context, ticketID, commentID, sessionID, body string) (*ReplyResult, error) {
	repoOwner, repoName, issueNumber, ok := tp.st.TicketLocation(ticketID)
	if !ok {
		return nil, fmt.Errorf("no location recorded for ticket %q — run scan first", ticketID)
	}

	tp.log.Debug("resolved ticket location",
		"ticket_id", ticketID,
		"repo_owner", repoOwner,
		"repo_name", repoName,
		"issue_number", issueNumber,
	)
	tp.log.Info("posting reply", "ticket_id", ticketID, "comment_id", commentID)

	if err := tp.gh.PostComment(ctx, repoOwner, repoName, issueNumber, body); err != nil {
		return nil, err
	}

	tp.st.MarkProcessed(commentID)
	tp.st.SetSession(ticketID, sessionID)
	tp.st.SetLastProcessedComment(ticketID, commentID)
	if err := tp.st.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	tp.log.Info("reply posted", "ticket_id", ticketID, "session_id", sessionID)

	return &ReplyResult{
		TicketID:  ticketID,
		CommentID: commentID,
		SessionID: sessionID,
	}, nil
}
