package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseProfileEntity(t *testing.T) {
	tests := []struct {
		name       string
		jsonData   string
		wantFirst  string
		wantLast   string
		wantURN    string
		wantPublic string
	}{
		{
			name: "direct profile fields",
			jsonData: `{
				"entityUrn": "urn:li:fsd_profile:ACoAAAA",
				"publicIdentifier": "johndoe",
				"firstName": "John",
				"lastName": "Doe",
				"headline": "Software Engineer"
			}`,
			wantFirst:  "John",
			wantLast:   "Doe",
			wantURN:    "urn:li:fsd_profile:ACoAAAA",
			wantPublic: "johndoe",
		},
		{
			name: "miniProfile nested",
			jsonData: `{
				"miniProfile": {
					"firstName": "Jane",
					"lastName": "Smith",
					"publicIdentifier": "janesmith",
					"entityUrn": "urn:li:member:12345",
					"occupation": "Product Manager"
				}
			}`,
			wantFirst:  "Jane",
			wantLast:   "Smith",
			wantURN:    "urn:li:member:12345",
			wantPublic: "janesmith",
		},
		{
			name: "occupation fallback to headline",
			jsonData: `{
				"firstName": "Bob",
				"lastName": "Builder",
				"occupation": "Construction Expert"
			}`,
			wantFirst: "Bob",
			wantLast:  "Builder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := &Profile{}
			err := parseProfileEntity(json.RawMessage(tt.jsonData), profile)
			if err != nil {
				t.Fatalf("parseProfileEntity error: %v", err)
			}

			if tt.wantFirst != "" && profile.FirstName != tt.wantFirst {
				t.Errorf("FirstName = %q, want %q", profile.FirstName, tt.wantFirst)
			}
			if tt.wantLast != "" && profile.LastName != tt.wantLast {
				t.Errorf("LastName = %q, want %q", profile.LastName, tt.wantLast)
			}
			if tt.wantURN != "" && profile.URN != tt.wantURN {
				t.Errorf("URN = %q, want %q", profile.URN, tt.wantURN)
			}
			if tt.wantPublic != "" && profile.PublicID != tt.wantPublic {
				t.Errorf("PublicID = %q, want %q", profile.PublicID, tt.wantPublic)
			}
		})
	}
}

func TestParseProfileFromResponse(t *testing.T) {
	tests := []struct {
		name      string
		resp      *VoyagerResponse
		wantErr   bool
		wantFirst string
	}{
		{
			name:    "nil response",
			resp:    nil,
			wantErr: true,
		},
		{
			name: "empty response",
			resp: &VoyagerResponse{
				Data:     nil,
				Included: nil,
			},
			wantErr: true,
		},
		{
			name: "profile in included",
			resp: &VoyagerResponse{
				Data: json.RawMessage(`{}`),
				Included: []json.RawMessage{
					json.RawMessage(`{
						"$type": "com.linkedin.voyager.identity.shared.MiniProfile",
						"entityUrn": "urn:li:fsd_profile:ACoAAAA",
						"firstName": "Alice",
						"lastName": "Wonderland"
					}`),
				},
			},
			wantErr:   false,
			wantFirst: "Alice",
		},
		{
			name: "profile in data",
			resp: &VoyagerResponse{
				Data: json.RawMessage(`{
					"firstName": "Charlie",
					"lastName": "Brown",
					"entityUrn": "urn:li:fsd_profile:test"
				}`),
				Included: nil,
			},
			wantErr:   false,
			wantFirst: "Charlie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, err := parseProfileFromResponse(tt.resp)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantFirst != "" && profile.FirstName != tt.wantFirst {
				t.Errorf("FirstName = %q, want %q", profile.FirstName, tt.wantFirst)
			}
		})
	}
}

func TestVoyagerResponsePaging(t *testing.T) {
	jsonData := `{
		"data": {},
		"included": [],
		"paging": {
			"count": 10,
			"start": 0,
			"total": 100,
			"links": [
				{"rel": "next", "href": "/path?start=10", "type": "application/json"}
			]
		}
	}`

	var resp VoyagerResponse
	if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if resp.Paging == nil {
		t.Fatal("Paging is nil")
	}
	if resp.Paging.Count != 10 {
		t.Errorf("Count = %d, want 10", resp.Paging.Count)
	}
	if resp.Paging.Total != 100 {
		t.Errorf("Total = %d, want 100", resp.Paging.Total)
	}
	if len(resp.Paging.Links) != 1 {
		t.Errorf("Links count = %d, want 1", len(resp.Paging.Links))
	}
}

func TestGetProfileActivityRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/feed/updates" {
			t.Errorf("path = %q, want /feed/updates", r.URL.Path)
		}

		query := r.URL.Query()
		if query.Get("profileId") != "johndoe" {
			t.Errorf("profileId = %q, want johndoe", query.Get("profileId"))
		}
		if query.Get("q") != "memberShareFeed" {
			t.Errorf("q = %q, want memberShareFeed", query.Get("q"))
		}
		if query.Get("moduleKey") != "member-share" {
			t.Errorf("moduleKey = %q, want member-share", query.Get("moduleKey"))
		}
		if query.Get("count") != "20" {
			t.Errorf("count = %q, want 20", query.Get("count"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"elements":[]},"included":[]}`))
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetProfileActivity(context.Background(), "johndoe", &FeedOptions{Limit: 20})
	if err != nil {
		t.Fatalf("GetProfileActivity error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestGetProfileActivityInvalidUsername(t *testing.T) {
	client := NewClient(WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}))

	_, err := client.GetProfileActivity(context.Background(), "bad/name", nil)
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

func TestParseProfileActivityFromResponse(t *testing.T) {
	resp := &VoyagerResponse{
		Data: json.RawMessage(`{
			"elements": [
				{
					"entityUrn": "urn:li:activity:2",
					"createdAt": 2000,
					"actor": {"urn": "urn:li:member:2", "name": {"text": "Jane Smith"}},
					"commentary": {"text": {"text": "Second post"}},
					"socialDetail": {"likes": 3, "comments": 4}
				}
			]
		}`),
		Included: []json.RawMessage{
			json.RawMessage(`{
				"entityUrn": "urn:li:activity:1",
				"createdAt": 1000,
				"actor": {"urn": "urn:li:member:1", "name": {"text": "John Doe"}},
				"commentary": {"text": {"text": "First post"}}
			}`),
			json.RawMessage(`{
				"entityUrn": "urn:li:activity:2",
				"createdAt": 2000,
				"actor": {"urn": "urn:li:member:2", "name": {"text": "Jane Smith"}},
				"commentary": {"text": {"text": "Second post"}}
			}`),
		},
	}

	items, err := parseProfileActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseProfileActivityFromResponse error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].URN != "urn:li:activity:2" {
		t.Errorf("first URN = %q, want urn:li:activity:2", items[0].URN)
	}
	if items[0].Post == nil || items[0].Post.Text != "Second post" {
		t.Fatalf("first post = %#v, want Second post", items[0].Post)
	}
	if items[0].Post.LikeCount != 3 || items[0].Post.CommentCount != 4 {
		t.Errorf("counts = %d/%d, want 3/4", items[0].Post.LikeCount, items[0].Post.CommentCount)
	}
}
