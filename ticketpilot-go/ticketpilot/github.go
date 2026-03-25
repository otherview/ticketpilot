package ticketpilot

import (
	"context"
	"fmt"
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
}

// NewGitHubClient creates a GitHubClient from config.
func NewGitHubClient(cfg *Config) GitHubClient {
	return &gitHubClient{
		gh:            gh.NewClient(nil).WithAuthToken(cfg.GitHubPAT),
		handle:        strings.ToLower(strings.TrimPrefix(cfg.GitHubHandle, "@")),
		owner:         cfg.projectOwner,
		ownerType:     cfg.projectOwnerType,
		projectNumber: cfg.projectNumber,
	}
}

// GetNextMention scans project items for the first unprocessed @handle mention.
// Iterates items in order and comments chronologically; returns as soon as one
// is found. Returns nil, nil when there is nothing pending.
func (c *gitHubClient) GetNextMention(
	ctx context.Context,
	since time.Time,
	isProcessed func(string) bool,
	sessionFor func(string) string,
) (*Mention, error) {
	items, err := c.listProjectItems(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing project items: %w", err)
	}

	for _, item := range items {
		ct := item.GetContentType()
		if ct == nil {
			continue
		}
		if *ct != gh.ProjectV2ItemContentTypeIssue && *ct != gh.ProjectV2ItemContentTypePullRequest {
			continue // skip DraftIssues — no comments to reply to
		}

		repoOwner, repoName, issueNum, title, _, err := extractContent(item)
		if err != nil {
			continue // skip unparseable items, don't block the rest
		}

		comments, err := c.listCommentsSince(ctx, repoOwner, repoName, issueNum, since)
		if err != nil {
			continue
		}

		// Prepend the issue/PR body as the first thread entry so the AI sees
		// the original ticket description as part of the conversation.
		thread := prependBody(item, comments)

		for _, comment := range comments {
			if isProcessed(comment.ID) {
				continue
			}
			if !strings.Contains(strings.ToLower(comment.Body), "@"+c.handle) {
				continue
			}

			ticketID := fmt.Sprintf("%s/%s#%d", repoOwner, repoName, issueNum)

			var sessionID *string
			if s := sessionFor(ticketID); s != "" {
				sessionID = &s
			}

			return &Mention{
				TicketID:      ticketID,
				CommentID:     comment.ID,
				Title:         title,
				Type:          string(*ct),
				RepoOwner:     repoOwner,
				RepoName:      repoName,
				IssueNumber:   issueNum,
				CommentAuthor: comment.Author,
				CommentBody:   comment.Body,
				Thread:        thread,
				SessionID:     sessionID,
			}, nil
		}
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

func (c *gitHubClient) listCommentsSince(ctx context.Context, owner, repo string, issueNum int, since time.Time) ([]Comment, error) {
	opts := &gh.IssueListCommentsOptions{
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

// prependBody inserts the issue or PR body as the first Comment in the thread
// so the AI receives the original ticket description as part of the conversation.
// If the body is empty the thread is returned unchanged.
func prependBody(item *gh.ProjectV2Item, comments []Comment) []Comment {
	content := item.GetContent()
	if content == nil {
		return comments
	}

	var id, author, body string
	var createdAt time.Time

	switch {
	case content.Issue != nil:
		body = content.Issue.GetBody()
		id = fmt.Sprintf("issue:%d", content.Issue.GetID())
		author = content.Issue.GetUser().GetLogin()
		createdAt = content.Issue.GetCreatedAt().Time
	case content.PullRequest != nil:
		body = content.PullRequest.GetBody()
		id = fmt.Sprintf("pr:%d", content.PullRequest.GetID())
		author = content.PullRequest.GetUser().GetLogin()
		createdAt = content.PullRequest.GetCreatedAt().Time
	}

	if body == "" {
		return comments
	}

	first := Comment{ID: id, Author: author, Body: body, CreatedAt: createdAt}
	return append([]Comment{first}, comments...)
}

// extractContent pulls owner, repo, issue number and title out of a project item.
// ProjectV2ItemContent is a union type — only Issue or PullRequest will be populated.
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
// Handles HTML URLs:  github.com/owner/repo/issues/N
// and API URLs:       api.github.com/repos/owner/repo/issues/N
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
