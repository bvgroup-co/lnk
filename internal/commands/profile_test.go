package commands

import (
	"bytes"
	"encoding/json"
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

func TestOutputActivityItemsJSONIncludesEmptyData(t *testing.T) {
	output := captureStdout(t, func() {
		err := outputActivityItems(true, nil, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputActivityItems error: %v", err)
		}
	})

	var response struct {
		Success bool               `json:"success"`
		Data    []api.ActivityItem `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("Unmarshal output error: %v", err)
	}
	if !response.Success {
		t.Errorf("Success = false, want true")
	}
	if response.Data == nil {
		t.Fatalf("Data is nil, want empty slice")
	}
	if len(response.Data) != 0 {
		t.Errorf("len(Data) = %d, want 0", len(response.Data))
	}
	if !strings.Contains(output, `"data": []`) {
		t.Errorf("output missing empty data array: %s", output)
	}
}

func TestOutputErrorJSONShape(t *testing.T) {
	output := captureStdout(t, func() {
		if err := outputJSON(api.Response[any]{
			Success: false,
			Error: &api.Error{
				Code:    api.ErrCodeUnsupported,
				Message: `LinkedIn Web UI matching for category "posts" is not currently implemented.`,
			},
		}); err != nil {
			t.Fatalf("outputJSON error: %v", err)
		}
	})

	var response struct {
		Success bool       `json:"success"`
		Data    []any      `json:"data"`
		Error   *api.Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatalf("Unmarshal output error: %v", err)
	}
	if response.Success {
		t.Error("Success = true, want false")
	}
	if response.Data != nil {
		t.Errorf("Data = %v, want nil", response.Data)
	}
	if response.Error == nil || response.Error.Code != api.ErrCodeUnsupported {
		t.Fatalf("Error = %#v, want unsupported error", response.Error)
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

func TestOutputActivityItemsTextPrintsCommentAndParent(t *testing.T) {
	output := captureStdout(t, func() {
		err := outputActivityItems(false, []api.ActivityItem{
			{
				URN:             "urn:li:comment:(urn:li:activity:1,2)",
				Text:            "actual comment",
				URL:             "https://www.linkedin.com/feed/update/urn:li:activity:1",
				ContentCategory: api.RecentActivityCategoryComments,
				CommentedOnText: "parent post",
			},
		}, "No recent activity found.")
		if err != nil {
			t.Fatalf("outputActivityItems error: %v", err)
		}
	})

	for _, want := range []string{"Comment: actual comment", "Parent: parent post", "URL: https://www.linkedin.com/feed/update/urn:li:activity:1", "URN: urn:li:comment:(urn:li:activity:1,2)"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q: %s", want, output)
		}
	}
	if strings.Contains(output, "Post:") {
		t.Errorf("comment output contains Post label: %s", output)
	}
}

func TestOutputActivityDebugShapeJSON(t *testing.T) {
	output := captureStdout(t, func() {
		err := outputActivityDebugShape(true, &api.ActivityDebugShape{
			EndpointPath:  "/feed/updatesV2",
			Query:         []string{"count=10", "q=memberShareFeed"},
			Status:        200,
			TopLevelKeys:  []string{"data", "included", "paging"},
			DataCount:     1,
			IncludedCount: 1,
			ExampleTypes:  []string{"com.linkedin.voyager.feed.Update"},
			PagingKeys:    []string{"count", "links", "start"},
			HasNextLink:   true,
		})
		if err != nil {
			t.Fatalf("outputActivityDebugShape error: %v", err)
		}
	})

	for _, secret := range []string{"li_at", "JSESSIONID", "csrf", "Cookie", "hello world"} {
		if strings.Contains(output, secret) {
			t.Errorf("debug shape leaked %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, `"endpointPath": "/feed/updatesV2"`) {
		t.Errorf("output missing endpoint path: %s", output)
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

func TestProfileActivitySafetyFlags(t *testing.T) {
	cmd := newProfileActivityCmd()

	for _, flagName := range []string{"experimental-local-filter", "debug-shape"} {
		flag := cmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Fatalf("%s flag missing", flagName)
		}
		if flag.DefValue != "false" {
			t.Errorf("%s default = %q, want false", flagName, flag.DefValue)
		}
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
