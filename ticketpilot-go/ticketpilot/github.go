package ticketpilot

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/v84/github"
)

// gitHubClient is the concrete implementation of GitHubClient.
type gitHubClient struct {
	gh            *gh.Client
	handle        string
	owner         string
	ownerType     string
	projectNumber int
	log           *slog.Logger
}

// NewGitHubClient creates a GitHubClient from config.
func NewGitHubClient(cfg *Config, logger *slog.Logger) GitHubClient {
	return &gitHubClient{
		gh:            gh.NewClient(nil).WithAuthToken(cfg.GitHubPAT),
		handle:        strings.ToLower(strings.TrimPrefix(cfg.GitHubHandle, "@")),
		owner:         cfg.projectOwner,
		ownerType:     cfg.projectOwnerType,
		projectNumber: cfg.projectNumber,
		log:           logger,
	}
}

// GetNextMention scans project items for a pending @handle mention.
//
// For each ticket it:
//  1. Pre-filters using issue.UpdatedAt <= cutoff to skip API calls for inactive tickets.
//  2. Fetches comments descending (newest first) since cutoff.
//  3. Finds the first non-bot @handle mention in desc order = last chronological mention.
//  4. Builds thread = reverse(comments[mentionIdx:]) — chronological up to and including
//     the mention, no post-mention noise.
//  5. Prepends the issue body when there is no prior session (new conversation context).
//
// Returns nil, nil when nothing is pending.
func (c *gitHubClient) GetNextMention(
	ctx context.Context,
	cutoffFor func(string) time.Time,
	sessionFor func(string) string,
) (*Mention, error) {
	items, err := c.listProjectItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing project items: %w", err)
	}

	c.log.Debug("project items fetched", "count", len(items))

	for _, item := range items {
		ct := item.GetContentType()
		if ct == nil {
			c.log.Debug("skipping item: no content type")
			continue
		}
		if *ct != gh.ProjectV2ItemContentTypeIssue && *ct != gh.ProjectV2ItemContentTypePullRequest {
			c.log.Debug("skipping item: not issue or PR", "type", string(*ct))
			continue
		}

		// Skip closed issues and closed/merged PRs.
		content := item.GetContent()
		if content != nil {
			if content.Issue != nil && content.Issue.GetState() == "closed" {
				c.log.Debug("skipping item: issue is closed", "title", content.Issue.GetTitle())
				continue
			}
			if content.PullRequest != nil && content.PullRequest.GetState() == "closed" {
				c.log.Debug("skipping item: PR is closed/merged", "title", content.PullRequest.GetTitle())
				continue
			}
		}

		repoOwner, repoName, issueNum, title, _, err := extractContent(item)
		if err != nil {
			c.log.Debug("skipping item: could not extract content", "err", err)
			continue
		}

		ticketID := fmt.Sprintf("%s/%s#%d", repoOwner, repoName, issueNum)
		cutoff := cutoffFor(ticketID)

		// Pre-filter: skip tickets not updated since the cutoff — avoids API calls.
		var updatedAt time.Time
		if content != nil {
			if content.Issue != nil {
				updatedAt = content.Issue.GetUpdatedAt().Time
			} else if content.PullRequest != nil {
				updatedAt = content.PullRequest.GetUpdatedAt().Time
			}
		}
		if !updatedAt.IsZero() && !updatedAt.After(cutoff) {
			c.log.Debug("skipping item: not updated since cutoff", "title", title, "updated_at", updatedAt, "cutoff", cutoff)
			continue
		}

		c.log.Debug("scanning item", "title", title, "repo", repoOwner+"/"+repoName, "issue", issueNum, "cutoff", cutoff)

		comments, err := c.listCommentsDesc(ctx, repoOwner, repoName, issueNum, cutoff)
		if err != nil {
			c.log.Debug("skipping item: could not list comments", "err", err)
			continue
		}

		c.log.Debug("comments fetched", "count", len(comments))

		// Find the first non-bot @handle mention in descending order.
		// First hit = last chronological mention (most recent).
		mentionIdx := -1
		for i, cm := range comments {
			if !strings.EqualFold(cm.Author, c.handle) &&
				strings.Contains(strings.ToLower(cm.Body), "@"+c.handle) {
				mentionIdx = i
				break
			}
		}

		hasSession := sessionFor(ticketID) != ""

		if mentionIdx < 0 {
			// No mention in comments — check if the issue body itself is the trigger.
			// Only applies when there is no prior session (fresh conversation).
			if hasSession {
				c.log.Debug("skipping item: no new mention, session exists", "title", title)
				continue
			}
			bodyComment := issueBodyComment(content)
			if bodyComment.Body == "" || !strings.Contains(strings.ToLower(bodyComment.Body), "@"+c.handle) {
				c.log.Debug("skipping item: no mention found", "title", title)
				continue
			}
			var issueCreatedAt time.Time
			if content != nil && content.Issue != nil {
				issueCreatedAt = content.Issue.GetCreatedAt().Time
			} else if content != nil && content.PullRequest != nil {
				issueCreatedAt = content.PullRequest.GetCreatedAt().Time
			}
			if !issueCreatedAt.After(cutoff) {
				c.log.Debug("skipping item: issue body mention predates cutoff", "title", title)
				continue
			}
			c.log.Debug("issue body is the trigger", "title", title)
			return &Mention{
				TicketID:      ticketID,
				CommentID:     bodyComment.ID,
				Title:         title,
				Type:          string(*ct),
				RepoOwner:     repoOwner,
				RepoName:      repoName,
				IssueNumber:   issueNum,
				CommentAuthor: bodyComment.Author,
				CommentBody:   bodyComment.Body,
				Thread:        []Comment{bodyComment},
				SessionID:     nil,
			}, nil
		}

		// Build thread: reverse so it is chronological (oldest → mention).
		// Only includes comments up to and including the mention — post-mention
		// comments are excluded to avoid confusing the AI with unrelated noise.
		thread := reverseComments(comments[mentionIdx:])

		// For a new conversation (no session) prepend the issue body so the AI
		// has the original ticket description as context.
		if !hasSession {
			if bodyComment := issueBodyComment(content); bodyComment.Body != "" {
				thread = append([]Comment{bodyComment}, thread...)
			}
		}

		mentionEntry := comments[mentionIdx]
		var sessionID *string
		if s := sessionFor(ticketID); s != "" {
			sessionID = &s
		}

		c.log.Debug("mention found",
			"title", title,
			"comment_id", mentionEntry.ID,
			"has_session", sessionID != nil,
			slog.Int("thread_length", len(thread)),
		)

		return &Mention{
			TicketID:      ticketID,
			CommentID:     mentionEntry.ID,
			Title:         title,
			Type:          string(*ct),
			RepoOwner:     repoOwner,
			RepoName:      repoName,
			IssueNumber:   issueNum,
			CommentAuthor: mentionEntry.Author,
			CommentBody:   mentionEntry.Body,
			Thread:        thread,
			SessionID:     sessionID,
		}, nil
	}

	return nil, nil
}

// PostComment posts a reply on the given issue or PR.
func (c *gitHubClient) PostComment(ctx context.Context, repoOwner, repoName string, issueNumber int, body string) error {
	_, _, err := c.gh.Issues.CreateComment(
		ctx,
		repoOwner,
		repoName,
		issueNumber,
		&gh.IssueComment{Body: gh.Ptr(body)},
	)
	if err != nil {
		return fmt.Errorf("posting comment: %w", err)
	}
	return nil
}

// CreateIssue creates a new issue in the target repository.
func (c *gitHubClient) CreateIssue(ctx context.Context, repoOwner, repoName, title, body string) (issueNumber int, issueID int64, issueURL string, err error) {
	req := &gh.IssueRequest{Title: gh.Ptr(title)}
	if strings.TrimSpace(body) != "" {
		req.Body = gh.Ptr(body)
	}
	issue, _, err := c.gh.Issues.Create(ctx, repoOwner, repoName, req)
	if err != nil {
		return 0, 0, "", fmt.Errorf("creating issue: %w", err)
	}
	return issue.GetNumber(), issue.GetID(), issue.GetHTMLURL(), nil
}

// AddIssueToProject adds an issue to the configured project.
func (c *gitHubClient) AddIssueToProject(ctx context.Context, issueID int64) (projectItemID int64, err error) {
	opts := &gh.AddProjectItemOptions{Type: gh.Ptr(gh.ProjectV2ItemContentTypeIssue), ID: gh.Ptr(issueID)}
	if c.ownerType == "org" {
		item, _, err := c.gh.Projects.AddOrganizationProjectItem(ctx, c.owner, c.projectNumber, opts)
		if err != nil {
			return 0, fmt.Errorf("adding issue to org project: %w", err)
		}
		return item.GetID(), nil
	}
	item, _, err := c.gh.Projects.AddUserProjectItem(ctx, c.owner, c.projectNumber, opts)
	if err != nil {
		return 0, fmt.Errorf("adding issue to user project: %w", err)
	}
	return item.GetID(), nil
}

// --- internal helpers ---

func (c *gitHubClient) listProjectItems(ctx context.Context) ([]*gh.ProjectV2Item, error) {
	// TODO: implement cursor-based pagination for projects with >100 items.
	opts := &gh.ListProjectItemsOptions{
		ListProjectsOptions: gh.ListProjectsOptions{
			ListProjectsPaginationOptions: gh.ListProjectsPaginationOptions{
				PerPage: 100,
			},
		},
	}

	if c.ownerType == "org" {
		items, _, err := c.gh.Projects.ListOrganizationProjectItems(ctx, c.owner, c.projectNumber, opts)
		return items, err
	}
	items, _, err := c.gh.Projects.ListUserProjectItems(ctx, c.owner, c.projectNumber, opts)
	return items, err
}

func (c *gitHubClient) listCommentsDesc(ctx context.Context, owner, repo string, issueNum int, since time.Time) ([]Comment, error) {
	opts := &gh.IssueListCommentsOptions{
		Sort:        gh.Ptr("created"),
		Direction:   gh.Ptr("desc"),
		Since:       &since,
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var all []Comment
	for {
		comments, resp, err := c.gh.Issues.ListComments(ctx, owner, repo, issueNum, opts)
		if err != nil {
			return nil, fmt.Errorf("listing comments on %s/%s#%d: %w", owner, repo, issueNum, err)
		}
		for _, cm := range comments {
			all = append(all, Comment{
				ID:        strconv.FormatInt(cm.GetID(), 10),
				Author:    cm.GetUser().GetLogin(),
				Body:      cm.GetBody(),
				CreatedAt: cm.GetCreatedAt().Time,
			})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// reverseComments returns a new slice with comments in reverse order.
func reverseComments(c []Comment) []Comment {
	out := make([]Comment, len(c))
	for i, v := range c {
		out[len(c)-1-i] = v
	}
	return out
}

// issueBodyComment builds a synthetic Comment from the issue or PR body.
// Returns a zero-value Comment if content is nil or the body is empty.
func issueBodyComment(content *gh.ProjectV2ItemContent) Comment {
	if content == nil {
		return Comment{}
	}
	switch {
	case content.Issue != nil:
		return Comment{
			ID:        fmt.Sprintf("issue:%d", content.Issue.GetID()),
			Author:    content.Issue.GetUser().GetLogin(),
			Body:      content.Issue.GetBody(),
			CreatedAt: content.Issue.GetCreatedAt().Time,
		}
	case content.PullRequest != nil:
		return Comment{
			ID:        fmt.Sprintf("pr:%d", content.PullRequest.GetID()),
			Author:    content.PullRequest.GetUser().GetLogin(),
			Body:      content.PullRequest.GetBody(),
			CreatedAt: content.PullRequest.GetCreatedAt().Time,
		}
	}
	return Comment{}
}

// extractContent pulls owner, repo, issue number and title out of a project item.
func extractContent(item *gh.ProjectV2Item) (owner, repo string, number int, title, htmlURL string, err error) {
	content := item.GetContent()
	if content == nil {
		return "", "", 0, "", "", fmt.Errorf("item has no content")
	}

	switch {
	case content.Issue != nil:
		title = content.Issue.GetTitle()
		number = content.Issue.GetNumber()
		htmlURL = content.Issue.GetHTMLURL()
		if htmlURL == "" {
			htmlURL = content.Issue.GetURL()
		}
	case content.PullRequest != nil:
		title = content.PullRequest.GetTitle()
		number = content.PullRequest.GetNumber()
		htmlURL = content.PullRequest.GetHTMLURL()
		if htmlURL == "" {
			htmlURL = content.PullRequest.GetURL()
		}
	default:
		return "", "", 0, "", "", fmt.Errorf("content is neither Issue nor PullRequest")
	}

	if htmlURL == "" {
		return "", "", 0, "", "", fmt.Errorf("no URL available on content")
	}

	owner, repo, parsedNum, err := parseGitHubURL(htmlURL)
	if err != nil {
		return "", "", 0, "", "", err
	}
	if number == 0 {
		number = parsedNum
	}

	return owner, repo, number, title, htmlURL, nil
}

// parseGitHubURL extracts owner, repo and issue number from a GitHub URL.
func parseGitHubURL(rawURL string) (owner, repo string, number int, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	// API URL: /repos/owner/repo/issues/N
	if len(parts) >= 5 && parts[0] == "repos" {
		n, convErr := strconv.Atoi(parts[4])
		if convErr != nil {
			return "", "", 0, fmt.Errorf("invalid issue number in %q", rawURL)
		}
		return parts[1], parts[2], n, nil
	}

	// HTML URL: /owner/repo/issues/N  or  /owner/repo/pull/N
	if len(parts) >= 4 {
		n, convErr := strconv.Atoi(parts[3])
		if convErr != nil {
			return "", "", 0, fmt.Errorf("invalid issue number in %q", rawURL)
		}
		return parts[0], parts[1], n, nil
	}

	return "", "", 0, fmt.Errorf("unrecognized GitHub URL: %q", rawURL)
}
