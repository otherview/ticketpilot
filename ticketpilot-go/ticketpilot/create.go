package ticketpilot

import (
	"context"
	"fmt"
	"strings"
)

// Create creates an issue in the target repository and adds it to the configured project.
func (tp *TicketPilot) Create(ctx context.Context, repoOwner, repoName, title, body string) (*CreateResult, error) {
	repoOwner = strings.TrimSpace(repoOwner)
	repoName = strings.TrimSpace(repoName)
	title = strings.TrimSpace(title)
	if repoOwner == "" || repoName == "" {
		return nil, fmt.Errorf("repo owner and repo name are required")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	issueNumber, issueID, issueURL, err := tp.gh.CreateIssue(ctx, repoOwner, repoName, title, body)
	if err != nil {
		return nil, err
	}

	projectItemID, err := tp.gh.AddIssueToProject(ctx, issueID)
	if err != nil {
		return nil, err
	}

	ticketID := fmt.Sprintf("%s/%s#%d", repoOwner, repoName, issueNumber)
	tp.st.RecordTicket(ticketID, repoOwner, repoName, issueNumber)
	if err := tp.st.Save(); err != nil {
		return nil, fmt.Errorf("saving state: %w", err)
	}

	return &CreateResult{
		TicketID:      ticketID,
		RepoOwner:     repoOwner,
		RepoName:      repoName,
		IssueNumber:   issueNumber,
		IssueURL:      issueURL,
		ProjectItemID: projectItemID,
	}, nil
}
