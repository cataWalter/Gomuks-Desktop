package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// AppVersion is the version shown on the About page. main assigns it from its
// Version variable; release builds can set that with -ldflags "-X main.Version=...".
var AppVersion = "dev"

const (
	latestReleaseURL = "https://api.github.com/repos/cataWalter/pinger/releases/latest"
	releasesPageURL  = "https://github.com/cataWalter/pinger/releases"
	maxReleaseNotes  = 4000
)

// UpdateStatus reports the result of a user-initiated release check.
type UpdateStatus struct {
	CurrentVersion  string `json:"currentVersion"`
	LatestVersion   string `json:"latestVersion"`
	ReleaseURL      string `json:"releaseUrl"`
	PublishedAt     string `json:"publishedAt"`
	ReleaseNotes    string `json:"releaseNotes"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Message         string `json:"message"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

// AppVersion returns the build version for the About page.
func (s *AppService) AppVersion() string {
	return AppVersion
}

// CheckForUpdates queries the latest published GitHub release. It is only called from
// the About page when the user asks, so the app makes no background update requests.
// Pinger does not download or install updates; the release URL is returned for manual install.
func (s *AppService) CheckForUpdates() (UpdateStatus, error) {
	return checkForUpdates(s.ctx, &http.Client{Timeout: 15 * time.Second}, latestReleaseURL, AppVersion)
}

func checkForUpdates(ctx context.Context, client *http.Client, url, current string) (UpdateStatus, error) {
	status := UpdateStatus{CurrentVersion: current, ReleaseURL: releasesPageURL}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return status, fmt.Errorf("build update request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Pinger/"+current)
	response, err := client.Do(request)
	if err != nil {
		return status, fmt.Errorf("check for updates: %w", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		status.Message = "No published releases are available yet."
		return status, nil
	case response.StatusCode != http.StatusOK:
		return status, fmt.Errorf("check for updates: GitHub returned %s", response.Status)
	}
	var release githubRelease
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&release); err != nil {
		return status, fmt.Errorf("read release information: %w", err)
	}
	if strings.TrimSpace(release.TagName) == "" {
		return status, errors.New("release information has no version")
	}
	status.LatestVersion = release.TagName
	if release.HTMLURL != "" {
		status.ReleaseURL = release.HTMLURL
	}
	status.PublishedAt = release.PublishedAt
	status.ReleaseNotes = truncateRunes(strings.TrimSpace(release.Body), maxReleaseNotes)
	newer, comparable := isNewerVersion(release.TagName, current)
	switch {
	case !comparable:
		status.Message = "This build's version cannot be compared with the latest release."
	case newer:
		status.UpdateAvailable = true
		status.Message = fmt.Sprintf("Version %s is available.", release.TagName)
	default:
		status.Message = "Pinger is up to date."
	}
	return status, nil
}

// isNewerVersion reports whether latest is newer than current. The comparison is
// false when either version is not a dotted numeric version, such as "dev".
func isNewerVersion(latest, current string) (newer, comparable bool) {
	latestParts, okLatest := parseVersion(latest)
	currentParts, okCurrent := parseVersion(current)
	if !okLatest || !okCurrent {
		return false, false
	}
	for i := range max(len(latestParts), len(currentParts)) {
		var left, right int
		if i < len(latestParts) {
			left = latestParts[i]
		}
		if i < len(currentParts) {
			right = currentParts[i]
		}
		if left != right {
			return left > right, true
		}
	}
	return false, true
}

func parseVersion(version string) ([]int, bool) {
	value := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if before, _, found := strings.Cut(value, "-"); found {
		value = before
	}
	if before, _, found := strings.Cut(value, "+"); found {
		value = before
	}
	if value == "" {
		return nil, false
	}
	parts := strings.Split(value, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, true
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
