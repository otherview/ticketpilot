package tests

import (
	"context"
	"testing"

	"github.com/otherview/ticketpilot/ticketpilot"
)

var testCfg = &ticketpilot.Config{LookbackDays: 30}

// newMention is a test helper that builds a Mention with sensible defaults.
func newMention(ticketID, commentID, repoOwner, repoName string, issueNumber int) *ticketpilot.Mention {
	return &ticketpilot.Mention{
		TicketID:      ticketID,
		CommentID:     commentID,
		Title:         "Fix the thing",
		Type:          "Issue",
		RepoOwner:     repoOwner,
		RepoName:      repoName,
		IssueNumber:   issueNumber,
		CommentAuthor: "alice",
		CommentBody:   "@bot please help",
	}
}

// TestScan_NoPending: no mentions in the project → pending=false, LastRunAt updated.
func TestScan_NoPending(t *testing.T) {
	gh := &FakeGitHubClient{}
	st := newFakeState()

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pending {
		t.Error("expected Pending=false")
	}
	if st.lastRunAt == nil {
		t.Error("expected LastRunAt to be set after a clean scan")
	}
	if st.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestScan_MentionFound: a new mention exists → returned with correct fields,
// ticket location recorded in state.
func TestScan_MentionFound(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m}}
	st := newFakeState()

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	if result.Mention.CommentID != "c1" {
		t.Errorf("CommentID: want c1, got %s", result.Mention.CommentID)
	}
	if result.Mention.SessionID != nil {
		t.Errorf("expected SessionID=nil for a new ticket, got %q", *result.Mention.SessionID)
	}
	// ticket location must be recorded in state so Reply can use it
	gotOwner, gotRepo, gotNum, ok := st.TicketLocation("owner/repo#1")
	if !ok {
		t.Fatal("expected ticket to be recorded in state")
	}
	if gotOwner != "owner" || gotRepo != "repo" || gotNum != 1 {
		t.Errorf("ticket location: want owner/repo#1, got %s/%s#%d", gotOwner, gotRepo, gotNum)
	}
	if st.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestScan_SkipsProcessedComment: the only mention is already processed →
// scan returns pending=false.
func TestScan_SkipsProcessedComment(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m}}
	st := newFakeState()
	st.MarkProcessed("c1")

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pending {
		t.Error("expected Pending=false — processed comment must be skipped")
	}
}

// TestScan_ReturnsFirstUnprocessed: two mentions; first is processed →
// scan returns the second one.
func TestScan_ReturnsFirstUnprocessed(t *testing.T) {
	m1 := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	m2 := newMention("owner/repo#2", "c2", "owner", "repo", 2)
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m1, m2}}
	st := newFakeState()
	st.MarkProcessed("c1")

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	if result.Mention.CommentID != "c2" {
		t.Errorf("expected comment c2, got %s", result.Mention.CommentID)
	}
}

// TestScan_NoSession_IncludesThread: no prior session → full thread is included
// so the caller has context to start a new conversation.
func TestScan_NoSession_IncludesThread(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	m.Thread = []ticketpilot.Comment{
		{ID: "c0", Author: "bob", Body: "first comment"},
		{ID: "c1", Author: "alice", Body: "@bot please help"},
	}
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m}}
	st := newFakeState()

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	if result.Mention.SessionID != nil {
		t.Error("expected SessionID=nil for a new ticket")
	}
	if len(result.Mention.Thread) != 2 {
		t.Errorf("expected full thread (2 comments) when no session, got %d", len(result.Mention.Thread))
	}
}

// TestScan_ExistingSession_DeltaThread: session exists → only comments after
// the last processed comment are returned (the delta).
func TestScan_ExistingSession_DeltaThread(t *testing.T) {
	m := newMention("owner/repo#1", "c3", "owner", "repo", 1)
	m.Thread = []ticketpilot.Comment{
		{ID: "c0", Author: "alice", Body: "first comment"},
		{ID: "c1", Author: "alice", Body: "@bot first ask"},   // bot replied to this
		{ID: "c2", Author: "bot", Body: "bot reply"},
		{ID: "c2b", Author: "bob", Body: "something else"},   // delta starts here
		{ID: "c3", Author: "carol", Body: "@bot second ask"}, // the new mention
	}
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m}}
	st := newFakeState()
	st.SetSession("owner/repo#1", "session-abc")
	st.SetLastProcessedComment("owner/repo#1", "c1") // bot last replied to c1

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	if result.Mention.SessionID == nil || *result.Mention.SessionID != "session-abc" {
		t.Fatalf("expected session-abc, got %v", result.Mention.SessionID)
	}
	// delta: only c2, c2b, c3 (everything after c1)
	if len(result.Mention.Thread) != 3 {
		t.Errorf("expected 3 delta comments, got %d", len(result.Mention.Thread))
	}
	if result.Mention.Thread[0].ID != "c2" {
		t.Errorf("expected delta to start at c2, got %s", result.Mention.Thread[0].ID)
	}
}

// TestScan_ExistingSession_AnchorNotInWindow: anchor is outside the lookback
// window → thread is empty (session has the context).
func TestScan_ExistingSession_AnchorNotInWindow(t *testing.T) {
	m := newMention("owner/repo#1", "c5", "owner", "repo", 1)
	m.Thread = []ticketpilot.Comment{
		{ID: "c4", Author: "bob", Body: "new comment"},
		{ID: "c5", Author: "carol", Body: "@bot help"},
	}
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m}}
	st := newFakeState()
	st.SetSession("owner/repo#1", "session-abc")
	st.SetLastProcessedComment("owner/repo#1", "c1") // c1 not in thread (outside window)

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	if len(result.Mention.Thread) != 0 {
		t.Errorf("expected empty thread when anchor outside window, got %d comments", len(result.Mention.Thread))
	}
}

// TestReply_Success: ticket recorded in state → comment posted, comment marked
// processed, session stored.
func TestReply_Success(t *testing.T) {
	gh := &FakeGitHubClient{}
	st := newFakeState()
	st.RecordTicket("owner/repo#1", "owner", "repo", 1)

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Reply(
		context.Background(), "owner/repo#1", "c1", "session-xyz", "Here is my reply",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TicketID != "owner/repo#1" {
		t.Errorf("TicketID: want owner/repo#1, got %s", result.TicketID)
	}
	// comment was posted to the right place
	if len(gh.PostedComments) != 1 {
		t.Fatalf("expected 1 posted comment, got %d", len(gh.PostedComments))
	}
	p := gh.PostedComments[0]
	if p.RepoOwner != "owner" || p.RepoName != "repo" || p.IssueNumber != 1 {
		t.Errorf("posted to wrong location: %+v", p)
	}
	if p.Body != "Here is my reply" {
		t.Errorf("unexpected body: %q", p.Body)
	}
	// state updated
	if !st.IsProcessed("c1") {
		t.Error("expected c1 to be marked processed")
	}
	if st.SessionFor("owner/repo#1") != "session-xyz" {
		t.Error("expected session to be stored")
	}
	if st.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestReply_UnknownTicket: ticket was never scanned → Reply returns an error.
func TestReply_UnknownTicket(t *testing.T) {
	gh := &FakeGitHubClient{}
	st := newFakeState()

	_, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Reply(
		context.Background(), "owner/repo#99", "c1", "session-xyz", "reply",
	)

	if err == nil {
		t.Error("expected an error for an unknown ticket")
	}
}

// TestScanReplyFlow: full round-trip — Scan finds a mention, Reply posts a
// response, a second Scan skips the now-processed comment.
func TestScanReplyFlow(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	gh := &FakeGitHubClient{Mentions: []*ticketpilot.Mention{m}}
	st := newFakeState()
	tp := ticketpilot.New(gh, st, testCfg, newTestLogger(t))
	ctx := context.Background()

	// first scan — finds the mention
	scan1, err := tp.Scan(ctx)
	if err != nil {
		t.Fatalf("scan 1 failed: %v", err)
	}
	if !scan1.Pending {
		t.Fatal("scan 1: expected Pending=true")
	}
	if scan1.Mention.CommentID != "c1" {
		t.Fatalf("scan 1: expected comment c1, got %s", scan1.Mention.CommentID)
	}

	// reply
	_, err = tp.Reply(ctx, scan1.Mention.TicketID, scan1.Mention.CommentID, "session-1", "Thanks!")
	if err != nil {
		t.Fatalf("reply failed: %v", err)
	}

	// second scan — comment is now processed, nothing pending
	scan2, err := tp.Scan(ctx)
	if err != nil {
		t.Fatalf("scan 2 failed: %v", err)
	}
	if scan2.Pending {
		t.Error("scan 2: expected Pending=false — comment was already processed")
	}
}
