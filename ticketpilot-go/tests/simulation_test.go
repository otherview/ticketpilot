package tests

import (
	"context"
	"testing"
	"time"

	"github.com/otherview/ticketpilot/ticketpilot"
)

var testCfg = &ticketpilot.Config{}

// anchor is used as the startedAt time in tests. Comments must have
// CreatedAt after anchor to be visible to the scanner.
var anchor = time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

// at returns a time N seconds after anchor.
func at(offsetSeconds int) time.Time {
	return anchor.Add(time.Duration(offsetSeconds) * time.Second)
}

// newMention builds a Mention. The single-entry thread uses at(1) as CreatedAt
// so it falls after the default anchor startedAt.
func newMention(ticketID, commentID, repoOwner, repoName string, issueNumber int) *ticketpilot.Mention {
	m := &ticketpilot.Mention{
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
	m.Thread = []ticketpilot.Comment{
		{ID: commentID, Author: m.CommentAuthor, Body: m.CommentBody, CreatedAt: at(1)},
	}
	return m
}

// newFakeGH builds a FakeGitHubClient with handle "bot".
func newFakeGH(mentions ...*ticketpilot.Mention) *FakeGitHubClient {
	return &FakeGitHubClient{Handle: "bot", Mentions: mentions}
}

// newFakeStateWithAnchor returns a FakeStateStore with startedAt = anchor,
// ready for non-first-run tests.
func newFakeStateWithAnchor() *FakeStateStore {
	st := newFakeState()
	st.SetStartedAt(anchor)
	return st
}

// TestScan_FirstRun: startedAt is nil → scan initialises it and returns pending=false.
func TestScan_FirstRun(t *testing.T) {
	gh := newFakeGH()
	st := newFakeState() // startedAt is nil

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pending {
		t.Error("expected Pending=false on first run")
	}
	if st.StartedAt() == nil {
		t.Error("expected startedAt to be set after first run")
	}
	if st.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestScan_NoPending: startedAt set, no mentions → pending=false, state saved.
func TestScan_NoPending(t *testing.T) {
	gh := newFakeGH()
	st := newFakeStateWithAnchor()

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pending {
		t.Error("expected Pending=false")
	}
	if st.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestScan_MentionFound: a new mention exists → returned with correct fields,
// ticket location recorded in state.
func TestScan_MentionFound(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	gh := newFakeGH(m)
	st := newFakeStateWithAnchor()

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

// TestScan_BotAlreadyReplied: lastRepliedAt is set after the mention →
// no comments after cutoff → scan returns pending=false.
func TestScan_BotAlreadyReplied(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	// mention at at(1), reply recorded at at(2) — cutoff is at(2), mention is before cutoff
	gh := newFakeGH(m)
	st := newFakeStateWithAnchor()
	replyTime := at(2)
	st.lastReplied["owner/repo#1"] = &replyTime

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Pending {
		t.Error("expected Pending=false — comment predates lastRepliedAt cutoff")
	}
}

// TestScan_SkipsHandledTicket_ReturnsOther: first ticket's mention is before
// its lastRepliedAt, second ticket has an unhandled mention → scan returns second.
func TestScan_SkipsHandledTicket_ReturnsOther(t *testing.T) {
	m1 := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	// m1's comment is at at(1); set lastRepliedAt to at(2) so it's filtered
	m2 := newMention("owner/repo#2", "c3", "owner", "repo", 2)
	gh := newFakeGH(m1, m2)
	st := newFakeStateWithAnchor()
	replyTime := at(2)
	st.lastReplied["owner/repo#1"] = &replyTime

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	if result.Mention.TicketID != "owner/repo#2" {
		t.Errorf("expected ticket owner/repo#2, got %s", result.Mention.TicketID)
	}
}

// TestScan_NoSession_IncludesFullThread: no prior session → full thread up to
// and including the mention is returned (no post-mention comments).
func TestScan_NoSession_IncludesFullThread(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	m.Thread = []ticketpilot.Comment{
		{ID: "c0", Author: "bob", Body: "first comment", CreatedAt: at(1)},
		{ID: "c1", Author: "alice", Body: "@bot please help", CreatedAt: at(2)},
		{ID: "c2", Author: "carol", Body: "post-mention noise", CreatedAt: at(3)},
	}
	gh := newFakeGH(m)
	st := newFakeStateWithAnchor()

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
	// thread: c0 and c1 only — c2 is post-mention and excluded
	if len(result.Mention.Thread) != 2 {
		t.Errorf("expected 2 comments (up to mention), got %d", len(result.Mention.Thread))
	}
	if result.Mention.Thread[0].ID != "c0" {
		t.Errorf("expected thread[0]=c0, got %s", result.Mention.Thread[0].ID)
	}
	if result.Mention.Thread[1].ID != "c1" {
		t.Errorf("expected thread[1]=c1 (the mention), got %s", result.Mention.Thread[1].ID)
	}
}

// TestScan_ExistingSession_DeltaThread: session exists → only comments after
// lastRepliedAt cutoff are returned, up to and including the new mention.
func TestScan_ExistingSession_DeltaThread(t *testing.T) {
	m := newMention("owner/repo#1", "c3", "owner", "repo", 1)
	// lastRepliedAt = at(2), so cutoff = at(2)
	// c0..c2 are before cutoff; c2b and c3 are after cutoff
	m.Thread = []ticketpilot.Comment{
		{ID: "c0", Author: "alice", Body: "first comment", CreatedAt: at(1)},
		{ID: "c2b", Author: "bob", Body: "something", CreatedAt: at(3)},
		{ID: "c3", Author: "carol", Body: "@bot second ask", CreatedAt: at(4)},
		{ID: "c4", Author: "dave", Body: "post-mention noise", CreatedAt: at(5)},
	}
	gh := newFakeGH(m)
	st := newFakeStateWithAnchor()
	st.SetSession("owner/repo#1", "session-abc")
	replyTime := at(2)
	st.lastReplied["owner/repo#1"] = &replyTime

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
	// delta: c2b and c3 (c4 is post-mention, excluded)
	if len(result.Mention.Thread) != 2 {
		t.Errorf("expected 2 delta comments, got %d", len(result.Mention.Thread))
	}
	if result.Mention.Thread[0].ID != "c2b" {
		t.Errorf("expected delta to start at c2b, got %s", result.Mention.Thread[0].ID)
	}
	if result.Mention.Thread[1].ID != "c3" {
		t.Errorf("expected delta to end at c3 (the mention), got %s", result.Mention.Thread[1].ID)
	}
}

// TestScan_MultipleStackedMentions_LastOnly: multiple @mentions after cutoff →
// only the most recent one triggers a reply.
func TestScan_MultipleStackedMentions_LastOnly(t *testing.T) {
	m := newMention("owner/repo#1", "c3", "owner", "repo", 1)
	m.Thread = []ticketpilot.Comment{
		{ID: "c1", Author: "alice", Body: "@bot first ask", CreatedAt: at(1)},
		{ID: "c2", Author: "bob", Body: "noise", CreatedAt: at(2)},
		{ID: "c3", Author: "carol", Body: "@bot second ask", CreatedAt: at(3)},
	}
	gh := newFakeGH(m)
	st := newFakeStateWithAnchor()

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Scan(context.Background())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Pending {
		t.Fatal("expected Pending=true")
	}
	// Must return c3 (most recent mention), not c1
	if result.Mention.CommentID != "c3" {
		t.Errorf("expected CommentID=c3 (last mention), got %s", result.Mention.CommentID)
	}
}

// TestReply_Success: ticket recorded in state → comment posted, session and
// lastRepliedAt stored.
func TestReply_Success(t *testing.T) {
	gh := newFakeGH()
	st := newFakeStateWithAnchor()
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
	// session stored
	if st.SessionFor("owner/repo#1") != "session-xyz" {
		t.Error("expected session to be stored")
	}
	// lastRepliedAt stored
	if st.LastRepliedAt("owner/repo#1") == nil {
		t.Error("expected lastRepliedAt to be set after reply")
	}
	if st.SaveCalls != 1 {
		t.Errorf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestReply_UnknownTicket: ticket was never scanned → Reply returns an error.
func TestReply_UnknownTicket(t *testing.T) {
	gh := newFakeGH()
	st := newFakeStateWithAnchor()

	_, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Reply(
		context.Background(), "owner/repo#99", "c1", "session-xyz", "reply",
	)

	if err == nil {
		t.Error("expected an error for an unknown ticket")
	}
}

// TestCreate_Success: issue created, added to project, and recorded in state.
func TestCreate_Success(t *testing.T) {
	gh := newFakeGH()
	st := newFakeStateWithAnchor()

	result, err := ticketpilot.New(gh, st, testCfg, newTestLogger(t)).Create(
		context.Background(), "owner", "repo", "New tweet ticket", "body",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TicketID != "owner/repo#1" {
		t.Fatalf("ticket id: want owner/repo#1, got %s", result.TicketID)
	}
	if len(gh.CreatedIssues) != 1 {
		t.Fatalf("expected 1 created issue, got %d", len(gh.CreatedIssues))
	}
	if len(gh.ProjectAdds) != 1 {
		t.Fatalf("expected 1 project add, got %d", len(gh.ProjectAdds))
	}
	owner, repo, num, ok := st.TicketLocation("owner/repo#1")
	if !ok || owner != "owner" || repo != "repo" || num != 1 {
		t.Fatalf("ticket location not recorded correctly: %v %s/%s#%d", ok, owner, repo, num)
	}
	if st.SaveCalls != 1 {
		t.Fatalf("expected 1 Save call, got %d", st.SaveCalls)
	}
}

// TestScanReplyFlow: full round-trip — Scan finds a mention, Reply posts a
// response, a second Scan sees the mention is now before lastRepliedAt and skips it.
func TestScanReplyFlow(t *testing.T) {
	m := newMention("owner/repo#1", "c1", "owner", "repo", 1)
	gh := newFakeGH(m)
	st := newFakeStateWithAnchor()
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

	// reply — records lastRepliedAt = now (after the mention's CreatedAt)
	_, err = tp.Reply(ctx, scan1.Mention.TicketID, scan1.Mention.CommentID, "session-1", "Thanks!")
	if err != nil {
		t.Fatalf("reply failed: %v", err)
	}

	// second scan — c1 is before lastRepliedAt cutoff → nothing pending
	scan2, err := tp.Scan(ctx)
	if err != nil {
		t.Fatalf("scan 2 failed: %v", err)
	}
	if scan2.Pending {
		t.Error("scan 2: expected Pending=false — mention is before lastRepliedAt")
	}
}
