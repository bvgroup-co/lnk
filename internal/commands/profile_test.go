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
