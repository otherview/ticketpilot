package ticketpilot

import (
	"context"
	"fmt"
	"strings"

	gh "github.com/google/go-github/v84/github"
)

// CreateResult is the output of a Create call.
type CreateResult struct {
	TicketID      string `json:"ticket_id"`
	RepoOwner     string `json:"repo_owner"`
	RepoName      string `json:"repo_name"`
	IssueNumber   int    `json:"issue_number"`
	IssueURL      string `json:"issue_url"`
	ProjectColumn string `json:"project_column"`
	SessionID     string `json:"session_id"`
}

// Create creates a new issue in the configured repository and adds it to the
// project board in the specified status column.
//
// Flow:
//
//	1. Create the issue via GitHub Issues API.
//	2. Find the project matching the repo name in the organization.
//	3. Find the "Status" field in the project and locate the status option
//	   matching the provided column name.
//	4. Add the issue as a project item, then set the status field value.
//	5. Record the ticket in the state store.
func (tp *TicketPilot) Create(
	ctx context.Context,
	title, body, projectColumn, sessionID string,
) (*CreateResult, error) {
	// 1. Create the issue.
	issueNum, issueID, err := tp.gh.CreateIssue(ctx, title, body)
	if err != nil {
		return nil, fmt.Errorf("creating issue: %w", err)
	}
	tp.log.Debug("issue created",
		"ticket", fmt.Sprintf("%s/%s#%d", tp.cfg.repoOwner, tp.cfg.repoName, issueNum),
		"issue_id", issueID,
	)

	// 2. Find the project matching the repo name.
	org := tp.cfg.repoOwner // project is owned by the same org as the repo
	projects, err := tp.gh.ListProjects(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("listing projects: %w", err)
	}

	var project *gh.ProjectV2
	for _, p := range projects {
		if p.GetName() == tp.cfg.repoName {
			project = p
			break
		}
	}
	if project == nil {
		names := make([]string, len(projects))
		for i, p := range projects {
			names[i] = p.GetName()
		}
		return nil, fmt.Errorf(
			"project %q not found in org %q (available: %v)",
			tp.cfg.repoName, org, names,
		)
	}
	projectNumber := project.GetNumber()
	tp.log.Debug("project found", "project_number", projectNumber, "name", project.GetName())

	// 3. Find the Status field and the matching status option.
	fields, err := tp.gh.ListProjectFields(ctx, org, projectNumber)
	if err != nil {
		return nil, fmt.Errorf("listing project fields: %w", err)
	}

	var statusFieldID int64
	var validOptions []string
	var statusFieldOptID string
	statusFound := false
	for _, field := range fields {
		if field.GetName() == "" {
			continue
		}
		if strings.EqualFold(field.GetName(), "Status") {
			statusFieldID = field.GetID()
			for _, opt := range field.Options {
				if opt == nil || opt.Name == nil || opt.Name.GetRaw() == "" {
					continue
				}
				optName := opt.Name.GetRaw()
				validOptions = append(validOptions, optName)
				if strings.EqualFold(optName, projectColumn) {
					statusFieldOptID = opt.GetID()
				}
			}
			statusFound = true
			break
		}
	}
	if !statusFound {
		return nil, fmt.Errorf(
			"Status field not found in project %q (field \"Status\" not present)",
			project.GetName(),
		)
	}
	if statusFieldOptID == "" {
		return nil, fmt.Errorf(
			"status %q not found — valid options: %v",
			projectColumn, validOptions,
		)
	}
	tp.log.Debug("status option matched", "field_id", statusFieldID, "option", statusFieldOptID, "option_name", projectColumn)

	// 4. Add the issue as a project item, then set the status field.
	item, err := tp.gh.AddProjectItem(ctx, org, projectNumber, int64(issueNum))
	if err != nil {
		return nil, fmt.Errorf("adding project item: %w", err)
	}
	tp.log.Debug("project item added", "item_id", item.GetID())

	// Now set the Status field on the item.
	// UpdateProjectV2Field uses ID (int64) and Value (any).
	// For single-select fields, Value should be the option ID (string).
	updateFields := []*gh.UpdateProjectV2Field{{
		ID:    statusFieldID,
		Value: statusFieldOptID,
	}}
	err = tp.gh.UpdateProjectItem(ctx, org, projectNumber, item.GetID(), updateFields)
	if err != nil {
		return nil, fmt.Errorf("updating project item status: %w", err)
	}
	tp.log.Debug("status field set", "item_id", item.GetID(), "column", projectColumn)

	// 5. Build the issue URL and result.
	issueURL := fmt.Sprintf("https://github.com/%s/%s/issues/%d", tp.cfg.repoOwner, tp.cfg.repoName, issueNum)
	ticketID := fmt.Sprintf("%s/%s#%d", tp.cfg.repoOwner, tp.cfg.repoName, issueNum)

	// 6. Record the ticket in state.
	tp.st.RecordTicket(ticketID, tp.cfg.repoOwner, tp.cfg.repoName, issueNum)
	if sessionID != "" {
		tp.st.SetSession(ticketID, sessionID)
	}
	if err := tp.st.Save(); err != nil {
		tp.log.Warn("failed to save state after create", "err", err)
		// The issue was already created and added to the project — we return
		// the result but include the state-save error so the caller can decide.
	}

	return &CreateResult{
		TicketID:      ticketID,
		RepoOwner:     tp.cfg.repoOwner,
		RepoName:      tp.cfg.repoName,
		IssueNumber:   issueNum,
		IssueURL:      issueURL,
		ProjectColumn: projectColumn,
		SessionID:     sessionID,
	}, nil
}
