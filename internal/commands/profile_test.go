package commands

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pp/lnk/internal/api"
)

func TestOutputFeedItemsJSON(t *testing.T) {
	output := captureStdout(t, func() {
		err := outputFeedItems(true, []api.FeedItem{
			{
				URN: "urn:li:activity:1",
				Post: &api.Post{
					URN:  "urn:li:activity:1",
					Text: "hello world",
				},
			},
		}, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputFeedItems error: %v", err)
		}
	})

	if !strings.Contains(output, `"success": true`) {
		t.Errorf("output missing success: %s", output)
	}
	if !strings.Contains(output, `"text": "hello world"`) {
		t.Errorf("output missing post text: %s", output)
	}
}

func TestOutputFeedItemsText(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)
	output := captureStdout(t, func() {
		err := outputFeedItems(false, []api.FeedItem{
			{
				URN:       "urn:li:activity:1",
				CreatedAt: createdAt,
				Actor:     &api.Profile{FirstName: "John Doe"},
				Post:      &api.Post{Text: "hello world"},
			},
		}, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputFeedItems error: %v", err)
		}
	})

	for _, want := range []string{"From: John Doe", "Created: 2026-06-24 12:30:00", "Post: hello world", "URN: urn:li:activity:1"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}

func TestOutputFeedItemsEmpty(t *testing.T) {
	output := captureStdout(t, func() {
		err := outputFeedItems(false, nil, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputFeedItems error: %v", err)
		}
	})

	if strings.TrimSpace(output) != "No recent activity found." {
		t.Errorf("output = %q, want empty message", output)
	}
}

func TestOutputActivityItemsJSON(t *testing.T) {
	output := captureStdout(t, func() {
		err := outputActivityItems(true, []api.ActivityItem{
			{
				URN:  "urn:li:activity:1",
				Text: "hello world",
			},
		}, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputActivityItems error: %v", err)
		}
	})

	if !strings.Contains(output, `"success": true`) {
		t.Errorf("output missing success: %s", output)
	}
	if !strings.Contains(output, `"text": "hello world"`) {
		t.Errorf("output missing activity text: %s", output)
	}
}

func TestOutputActivityItemsText(t *testing.T) {
	createdAt := time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)
	output := captureStdout(t, func() {
		err := outputActivityItems(false, []api.ActivityItem{
			{
				URN:       "urn:li:activity:1",
				ActorName: "John Doe",
				Text:      "hello world",
				CreatedAt: createdAt,
				URL:       "https://www.linkedin.com/feed/update/urn:li:activity:1",
			},
		}, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputActivityItems error: %v", err)
		}
	})

	for _, want := range []string{"From: John Doe", "Created: 2026-06-24 12:30:00", "Post: hello world", "URL: https://www.linkedin.com/feed/update/urn:li:activity:1", "URN: urn:li:activity:1"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
}

func TestRecentActivityEmptyMessage(t *testing.T) {
	tests := []struct {
		category api.RecentActivityCategory
		want     string
	}{
		{category: api.RecentActivityCategoryAll, want: "No recent activity found."},
		{category: api.RecentActivityCategoryPosts, want: "No recent posts activity found."},
		{category: api.RecentActivityCategoryImages, want: "No recent images activity found."},
		{category: api.RecentActivityCategoryVideos, want: "No recent videos activity found."},
		{category: api.RecentActivityCategoryDocuments, want: "No recent documents activity found."},
		{category: api.RecentActivityCategoryEvents, want: "No recent events activity found."},
		{category: api.RecentActivityCategoryReactions, want: "No recent reactions activity found."},
		{category: api.RecentActivityCategoryComments, want: "No recent comments activity found."},
	}

	for _, tt := range tests {
		if got := recentActivityEmptyMessage(tt.category); got != tt.want {
			t.Errorf("recentActivityEmptyMessage(%q) = %q, want %q", tt.category, got, tt.want)
		}
	}
}

func TestProfileActivityCategoryFlagDefault(t *testing.T) {
	cmd := newProfileActivityCmd()
	flag := cmd.Flags().Lookup("category")
	if flag == nil {
		t.Fatal("category flag missing")
	}
	if flag.DefValue != string(api.RecentActivityCategoryAll) {
		t.Errorf("category default = %q, want all", flag.DefValue)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe error: %v", err)
	}
	os.Stdout = writer

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close error: %v", err)
	}
	os.Stdout = originalStdout

	var output bytes.Buffer
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatalf("io.Copy error: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close error: %v", err)
	}

	return output.String()
}
