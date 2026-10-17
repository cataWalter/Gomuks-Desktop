package desktop

import (
	"testing"
	"time"
)

func TestMediaCleanupFilterConvertsAgeAndValidatesType(t *testing.T) {
	now := time.Date(2026, time.March, 31, 12, 0, 0, 0, time.UTC)

	filter, err := mediaCleanupFilter(MediaCleanupRequest{RoomID: " !room:example.org ", OlderThanDays: 30, MediaType: "image"}, now)
	if err != nil {
		t.Fatalf("convert request: %v", err)
	}
	if filter.RoomID != "!room:example.org" || filter.MediaType != "image" {
		t.Fatalf("filter = %+v", filter)
	}
	if want := now.AddDate(0, 0, -30); !filter.OlderThan.Equal(want) {
		t.Fatalf("older-than = %s, want %s", filter.OlderThan, want)
	}

	all, err := mediaCleanupFilter(MediaCleanupRequest{}, now)
	if err != nil || !all.OlderThan.IsZero() || all.RoomID != "" || all.MediaType != "" {
		t.Fatalf("empty request should match everything, got %+v, %v", all, err)
	}

	if _, err := mediaCleanupFilter(MediaCleanupRequest{OlderThanDays: -1}, now); err == nil {
		t.Fatal("negative age should be rejected")
	}
	if _, err := mediaCleanupFilter(MediaCleanupRequest{MediaType: "gif"}, now); err == nil {
		t.Fatal("unknown media type should be rejected")
	}
}
