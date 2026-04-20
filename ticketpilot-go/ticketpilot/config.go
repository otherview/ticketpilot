package ticketpilot

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all TicketPilot configuration.
type Config struct {
	GitHubPAT    string
	GitHubHandle string
	ProjectURL   string
	StateFile    string
	GitHubRepo   string

	// parsed from ProjectURL during LoadConfig
	projectOwner     string
	projectOwnerType string
	projectNumber    int

	// parsed from GitHubRepo during LoadConfig
	repoOwner string
	repoName  string
}

// LoadConfig loads configuration from an env file then reads TICKETPILOT_*
// environment variables. Real env vars take precedence over the file.
// If envFile is empty, .env in the current working directory is tried.
func LoadConfig(envFile string) (*Config, error) {
	if envFile != "" {
		if err := loadEnvFile(envFile); err != nil {
			return nil, fmt.Errorf("loading env file %q: %w", envFile, err)
		}
	} else {
		if err := loadEnvFile(".env"); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("loading .env: %w", err)
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	cfg := &Config{
		GitHubPAT:    os.Getenv("TICKETPILOT_GITHUB_PAT"),
		GitHubHandle: strings.TrimPrefix(strings.ToLower(os.Getenv("TICKETPILOT_GITHUB_HANDLE")), "@"),
		ProjectURL:   os.Getenv("TICKETPILOT_PROJECT_URL"),
		StateFile:    os.Getenv("TICKETPILOT_STATE_FILE"),
		GitHubRepo:   os.Getenv("TICKETPILOT_REPO"),
	}

	if cfg.StateFile == "" {
		cfg.StateFile = "state.json"
	}
	cfg.StateFile = expandHome(cfg.StateFile, home)

	var missing []string
	if cfg.GitHubPAT == "" {
		missing = append(missing, "TICKETPILOT_GITHUB_PAT")
	}
	if cfg.GitHubHandle == "" {
		missing = append(missing, "TICKETPILOT_GITHUB_HANDLE")
	}
	if cfg.ProjectURL == "" {
		missing = append(missing, "TICKETPILOT_PROJECT_URL")
	}
	if cfg.GitHubRepo == "" {
		missing = append(missing, "TICKETPILOT_REPO")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf(
			"missing required config: %s\nSet via .env file (use --env-file to specify a path) or environment variables",
			strings.Join(missing, ", "),
		)
	}

	owner, ownerType, number, err := parseProjectURL(cfg.ProjectURL)
	if err != nil {
		return nil, fmt.Errorf("invalid TICKETPILOT_PROJECT_URL: %w", err)
	}
	cfg.projectOwner = owner
	cfg.projectOwnerType = ownerType
	cfg.projectNumber = number

	// Parse GitHubRepo as "owner/repo"
	cfg.repoOwner, cfg.repoName, err = parseGitHubRepo(cfg.GitHubRepo)
	if err != nil {
		return nil, fmt.Errorf("invalid TICKETPILOT_REPO: %w", err)
	}

	return cfg, nil
}

// loadEnvFile reads KEY=VALUE pairs from path and sets them as environment
// variables. Existing env vars are not overwritten. Blank lines and lines
// starting with # are ignored.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
	return scanner.Err()
}

// parseProjectURL extracts owner, ownerType and project number from a GitHub
// project URL. Handles:
//
//	https://github.com/users/<owner>/projects/<N>
//	https://github.com/orgs/<org>/projects/<N>
func parseProjectURL(rawURL string) (owner, ownerType string, number int, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", 0, fmt.Errorf("parsing URL %q: %w", rawURL, err)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// expect: ["users"|"orgs", <owner>, "projects", <N>]
	if len(parts) < 4 || parts[2] != "projects" {
		return "", "", 0, fmt.Errorf(
			"unrecognised GitHub project URL %q — expected https://github.com/users/<owner>/projects/<N> or https://github.com/orgs/<org>/projects/<N>",
			rawURL,
		)
	}

	switch parts[0] {
	case "users":
		ownerType = "user"
	case "orgs":
		ownerType = "org"
	default:
		return "", "", 0, fmt.Errorf(
			"unrecognised GitHub project URL %q — path must start with /users/ or /orgs/",
			rawURL,
		)
	}

	owner = parts[1]
	number, err = strconv.Atoi(parts[3])
	if err != nil {
		return "", "", 0, fmt.Errorf("invalid project number in %q", rawURL)
	}

	return owner, ownerType, number, nil
}

func expandHome(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// parseGitHubRepo parses "owner/repo" into separate owner and repo components.
func parseGitHubRepo(repo string) (owner, name string, err error) {
	parts := strings.Split(strings.TrimSpace(repo), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format \"owner/repo\", got %q", repo)
	}
	return parts[0], parts[1], nil
}

// ParseGitHubRepo populates the repoOwner and repoName fields from the
// GitHubRepo config value.  This is intended for test use where Config
// instances are constructed directly without going through LoadConfig.
func (c *Config) ParseGitHubRepo() error {
	var err error
	c.repoOwner, c.repoName, err = parseGitHubRepo(c.GitHubRepo)
	return err
}
