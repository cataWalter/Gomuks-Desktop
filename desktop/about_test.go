package desktop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsNewerVersionComparesDottedVersions(t *testing.T) {
	cases := []struct {
		latest, current string
		newer, ok       bool
	}{
		{"v1.2.0", "1.1.9", true, true},
		{"1.2.0", "v1.2.0", false, true},
		{"1.10.0", "1.9.3", true, true},
		{"1.2.0-rc1", "1.1.0", true, true},
		{"1.0", "1.0.0", false, true},
		{"1.0.0", "dev", false, false},
		{"", "1.0.0", false, false},
	}
	for _, tc := range cases {
		newer, ok := isNewerVersion(tc.latest, tc.current)
		if newer != tc.newer || ok != tc.ok {
			t.Fatalf("isNewerVersion(%q, %q) = (%v, %v), want (%v, %v)", tc.latest, tc.current, newer, ok, tc.newer, tc.ok)
		}
	}
}

func TestCheckForUpdatesReportsAvailableRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("missing GitHub Accept header")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://example.org/release","body":"Notes","published_at":"2026-03-01T00:00:00Z"}`))
	}))
	defer server.Close()

	status, err := checkForUpdates(context.Background(), server.Client(), server.URL, "1.0.0")
	if err != nil {
		t.Fatalf("check for updates: %v", err)
	}
	if !status.UpdateAvailable || status.LatestVersion != "v2.0.0" || status.ReleaseURL != "https://example.org/release" {
		t.Fatalf("status = %+v", status)
	}
	if status.ReleaseNotes != "Notes" || status.PublishedAt == "" {
		t.Fatalf("release details = %+v", status)
	}
}

func TestCheckForUpdatesHandlesNoReleasesAndFailures(t *testing.T) {
	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	status, err := checkForUpdates(context.Background(), notFound.Client(), notFound.URL, "1.0.0")
	if err != nil || status.UpdateAvailable || status.Message == "" {
		t.Fatalf("no-release status = %+v, %v", status, err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	if _, err = checkForUpdates(context.Background(), broken.Client(), broken.URL, "1.0.0"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected server error, got %v", err)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer empty.Close()
	if _, err = checkForUpdates(context.Background(), empty.Client(), empty.URL, "1.0.0"); err == nil {
		t.Fatal("expected error for release without a version")
	}
}

func TestCheckForUpdatesDevBuildIsNotComparable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer server.Close()

	status, err := checkForUpdates(context.Background(), server.Client(), server.URL, "dev")
	if err != nil {
		t.Fatalf("check for updates: %v", err)
	}
	if status.UpdateAvailable || !strings.Contains(status.Message, "cannot be compared") {
		t.Fatalf("dev build status = %+v", status)
	}
}

func TestTruncateRunesKeepsUTF8Intact(t *testing.T) {
	if got := truncateRunes("ééé", 2); got != "éé…" {
		t.Fatalf("truncate = %q", got)
	}
	if got := truncateRunes("ok", 10); got != "ok" {
		t.Fatalf("short value changed to %q", got)
	}
}
