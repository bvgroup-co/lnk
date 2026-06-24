package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	recentActivityProfilePath = "/voyagerIdentityDashProfiles"
	recentActivityLegacyPath  = "/feed/updates"
	recentActivityUpdatesPath = "/feed/updatesV2"
)

func TestGetRecentActivityInvalidUsername(t *testing.T) {
	client := newTestClient(WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}))

	_, err := client.GetRecentActivity(context.Background(), "bad/name", nil)
	if err == nil {
		t.Fatal("expected error")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Errorf("code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
}

func TestGetRecentActivityResolvesProfileAndBuildsPrimaryRequest(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case recentActivityProfilePath:
			if r.URL.Query().Get("memberIdentity") != "johndoe" {
				t.Errorf("memberIdentity = %q, want johndoe", r.URL.Query().Get("memberIdentity"))
			}
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{
					"entityUrn": "urn:li:fsd_profile:abc123",
					"publicIdentifier": "johndoe",
					"firstName": "John",
					"lastName": "Doe"
				}]
			}`)
		case recentActivityUpdatesPath:
			query := r.URL.Query()
			if query.Get("q") != "memberShareFeed" {
				t.Errorf("q = %q, want memberShareFeed", query.Get("q"))
			}
			if query.Get("profileUrn") != "urn:li:fsd_profile:abc123" {
				t.Errorf("profileUrn = %q, want urn:li:fsd_profile:abc123", query.Get("profileUrn"))
			}
			if query.Get("count") != "20" {
				t.Errorf("count = %q, want 20", query.Get("count"))
			}
			if query.Get("start") != "5" {
				t.Errorf("start = %q, want 5", query.Get("start"))
			}
			writeJSON(t, w, `{
				"included": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:1",
					"createdAt": 2000,
					"actor": {"urn": "urn:li:member:1", "name": {"text": "John Doe"}},
					"commentary": {"text": {"text": "hello activity"}},
					"socialDetail": {"likes": 3, "comments": 4, "shares": 5}
				}]
			}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Start: 5})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Text != "hello activity" {
		t.Errorf("Text = %q, want hello activity", items[0].Text)
	}
	if items[0].URL != "https://www.linkedin.com/feed/update/urn:li:activity:1" {
		t.Errorf("URL = %q", items[0].URL)
	}
	if len(requests) != 2 {
		t.Errorf("requests = %v, want profile then activity", requests)
	}
}

func TestGetRecentActivityFallback(t *testing.T) {
	activityRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{
					"entityUrn": "urn:li:fsd_profile:abc123",
					"publicIdentifier": "johndoe",
					"firstName": "John"
				}]
			}`)
		case recentActivityUpdatesPath:
			activityRequests++
			w.WriteHeader(http.StatusInternalServerError)
		case recentActivityLegacyPath:
			activityRequests++
			if r.URL.Query().Get("profileId") != "johndoe" {
				t.Errorf("profileId = %q, want johndoe", r.URL.Query().Get("profileId"))
			}
			writeJSON(t, w, `{"data":{"elements":[]},"included":[]}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", nil)
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
	if activityRequests != 2 {
		t.Errorf("activityRequests = %d, want 2", activityRequests)
	}
}

func TestGetRecentActivityAuthErrorStopsFallback(t *testing.T) {
	activityRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			activityRequests++
			w.WriteHeader(http.StatusForbidden)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	_, err := client.GetRecentActivity(context.Background(), "johndoe", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeForbidden {
		t.Errorf("code = %q, want %q", apiErr.Code, ErrCodeForbidden)
	}
	if activityRequests != 1 {
		t.Errorf("activityRequests = %d, want 1", activityRequests)
	}
}

func TestGetRecentActivityMalformedActivityFallsBack(t *testing.T) {
	activityRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath:
			activityRequests++
			writeJSON(t, w, `{
				"included": [{"$type": "com.linkedin.voyager.feed.Update", "createdAt": 1000}]
			}`)
		case recentActivityLegacyPath:
			activityRequests++
			writeJSON(t, w, `{"data":{"elements":[]},"included":[]}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", nil)
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
	if activityRequests != 2 {
		t.Errorf("activityRequests = %d, want 2", activityRequests)
	}
}

func TestGetRecentActivityMalformedActivityReturnsServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			writeJSON(t, w, `{
				"included": [{"$type": "com.linkedin.voyager.feed.Update", "createdAt": 1000}]
			}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	_, err := client.GetRecentActivity(context.Background(), "johndoe", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeServerError {
		t.Errorf("code = %q, want %q", apiErr.Code, ErrCodeServerError)
	}
}

func TestParseRecentActivityFromResponse(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.ShareUpdate",
				"entityUrn": "urn:li:activity:2",
				"createdAt": 2000,
				"actor": {"urn": "urn:li:member:2", "name": {"text": "Jane Smith"}},
				"commentary": {"text": {"text": "Second post"}},
				"socialDetail": {"likes": 3, "comments": 4, "shares": 5}
			}]
		}`),
		Included: []json.RawMessage{
			[]byte(`{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"createdAt": 1000,
				"actor": {"urn": "urn:li:member:1", "name": {"text": "John Doe"}},
				"commentaryV2": {"text": "First post"}
			}`),
			[]byte(`{"$type":"com.linkedin.voyager.identity.Profile","entityUrn":"urn:li:fsd_profile:abc"}`),
			[]byte(`{"$type":"com.linkedin.voyager.feed.Update","entityUrn":"urn:li:activity:2"}`),
		},
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].URN != "urn:li:activity:2" {
		t.Errorf("first URN = %q, want urn:li:activity:2", items[0].URN)
	}
	if items[0].Text != "Second post" {
		t.Errorf("Text = %q, want Second post", items[0].Text)
	}
	if items[0].LikeCount != 3 || items[0].CommentCount != 4 || items[0].ShareCount != 5 {
		t.Errorf("counts = %d/%d/%d, want 3/4/5", items[0].LikeCount, items[0].CommentCount, items[0].ShareCount)
	}
}

func TestParseRecentActivityFromResponseIgnoresMissingType(t *testing.T) {
	resp := &VoyagerResponse{
		Included: []json.RawMessage{
			[]byte(`{"entityUrn":"urn:li:activity:1","createdAt":1000}`),
			[]byte(`{"$type":"com.linkedin.voyager.identity.Profile","entityUrn":"urn:li:fsd_profile:abc"}`),
		},
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestParseRecentActivityFromResponseMalformedCandidate(t *testing.T) {
	resp := &VoyagerResponse{
		Included: []json.RawMessage{
			[]byte(`{"$type":"com.linkedin.voyager.feed.Update","createdAt":1000}`),
		},
	}

	_, err := parseRecentActivityFromResponse(resp)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeServerError {
		t.Errorf("code = %q, want %q", apiErr.Code, ErrCodeServerError)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
