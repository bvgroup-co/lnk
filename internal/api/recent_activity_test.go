package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	recentActivityProfilePath   = "/voyagerIdentityDashProfiles"
	recentActivityGraphQLPath   = "/graphql"
	recentActivityLegacyPath    = "/feed/updates"
	recentActivityUpdatesPath   = "/feed/updatesV2"
	testProfilePostsQueryID     = "voyagerFeedDashProfileUpdates.testposts"
	testProfileCommentsQueryID  = "voyagerFeedDashProfileUpdates.testcomments"
	testProfileReactionsQueryID = "voyagerFeedDashProfileUpdates.testreactions"
	testCapturedProfileURN      = "urn:li:fsd_profile:ACoAAAxDENoBpm-rthvddZLqgjJIoK5fVzxHxrY"
	testLegacyProfileURN        = "urn:li:fs_profile:ACoAAAxDENoBpm-rthvddZLqgjJIoK5fVzxHxrY"
	testActivityURN1            = "urn:li:activity:1"
	testActivityURN2            = "urn:li:activity:2"
	testActivityURN             = "urn:li:activity:7475116029644414976"
	testCapturedActivityURN     = "urn:li:activity:7451004062705119233"
	testReactedToURN            = "urn:li:activity:999"
	testReactedToURL            = "https://www.linkedin.com/feed/update/urn:li:activity:999"
	testCommentedOnURN          = "urn:li:activity:998"
	testCommentedOnURL          = "https://www.linkedin.com/feed/update/urn:li:activity:998"
	testCommentURN              = "urn:li:comment:(urn:li:activity:998,123)"
	testCommentActorName        = "Jane Doe"
	testCommentText             = "Great post"
	testMemberURN               = "urn:li:member:123"
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

func TestParseRecentActivityCategory(t *testing.T) {
	for _, category := range []string{"all", "posts", "images", "videos", "documents", "events", "reactions", "comments"} {
		parsed, err := ParseRecentActivityCategory(category)
		if err != nil {
			t.Fatalf("ParseRecentActivityCategory(%q) error: %v", category, err)
		}
		if string(parsed) != category {
			t.Errorf("ParseRecentActivityCategory(%q) = %q", category, parsed)
		}
	}
}

func TestParseRecentActivityCategoryRejectsInvalid(t *testing.T) {
	_, err := ParseRecentActivityCategory("articles")
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
	want := `invalid category "articles"; allowed values: all, posts, images, videos, documents, events, reactions, comments`
	if apiErr.Message != want {
		t.Errorf("message = %q, want %q", apiErr.Message, want)
	}
}

func TestGetRecentActivityRejectsInvalidCategoryBeforeNetwork(t *testing.T) {
	client := newTestClient(WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}))

	_, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Category: "articles"})
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

func TestGetRecentActivityUnsupportedCategoriesByDefault(t *testing.T) {
	for _, category := range []RecentActivityCategory{RecentActivityCategoryImages, RecentActivityCategoryVideos, RecentActivityCategoryDocuments, RecentActivityCategoryEvents} {
		t.Run(string(category), func(t *testing.T) {
			client := newTestClient(WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}))

			_, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Category: category})
			if err == nil {
				t.Fatal("expected error")
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *Error, got %T", err)
			}
			if apiErr.Code != ErrCodeUnsupported {
				t.Errorf("code = %q, want %q", apiErr.Code, ErrCodeUnsupported)
			}
			if !strings.Contains(apiErr.Message, fmt.Sprintf("category %q is not currently implemented", category)) {
				t.Errorf("message = %q, want unsupported category message", apiErr.Message)
			}
		})
	}
}

func TestGetRecentActivityPostsUsesGraphQLEndpoint(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileResponse(t, w)
		case recentActivityGraphQLPath:
			assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{
				Username:   "johndoe",
				ProfileURN: "urn:li:fsd_profile:abc123",
				Count:      "20",
				Start:      "5",
			})
			writeJSON(t, w, `{
				"data": {"feedDashProfileUpdatesByMemberShareFeed": {"metadata": {"paginationToken": ""}, "elements": [{
					"$type": "com.linkedin.voyager.dash.feed.Update",
					"metadata": {"backendUrn": "`+testCapturedActivityURN+`", "shareUrn": "urn:li:ugcPost:7451004062705119233"},
					"commentary": {"text": {"text": "hello"}},
					"socialContent": {"shareUrl": "https://www.linkedin.com/posts/example"},
					"socialDetail": {"totalSocialActivityCounts": {"numLikes": 3, "numComments": 4, "numShares": 5}}
				}]}}
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("posts must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
		WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{ProfilePostsQueryID: testProfilePostsQueryID}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Start: 5, Category: RecentActivityCategoryPosts})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.URN != testCapturedActivityURN {
		t.Errorf("URN = %q", item.URN)
	}
	if item.RawURN != "" {
		t.Errorf("RawURN = %q, want empty for direct backendUrn-only post", item.RawURN)
	}
	if item.Type != "com.linkedin.voyager.dash.feed.Update" {
		t.Errorf("Type = %q", item.Type)
	}
	if item.Text != "hello" || item.URL != "https://www.linkedin.com/posts/example" {
		t.Errorf("text/url = %q/%q", item.Text, item.URL)
	}
	if item.LikeCount != 3 || item.CommentCount != 4 || item.ShareCount != 5 {
		t.Errorf("counts = %d/%d/%d, want 3/4/5", item.LikeCount, item.CommentCount, item.ShareCount)
	}
	if item.ContentCategory != RecentActivityCategoryPosts {
		t.Errorf("ContentCategory = %q, want posts", item.ContentCategory)
	}
	if strings.Join(requests, ",") != recentActivityProfilePath+","+recentActivityGraphQLPath {
		t.Errorf("requests = %v, want profile then GraphQL", requests)
	}
}

func TestGetRecentActivityPostsUsesCapturedQueryIDByDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileResponse(t, w)
		case recentActivityGraphQLPath:
			assertGraphQLRawQuery(t, r, &graphQLPostsRequest{
				ProfileURN: "urn:li:fsd_profile:abc123",
				QueryID:    defaultProfilePostsQueryID,
				Count:      "20",
				Start:      "0",
			})
			writeJSON(t, w, `{"data":{"feedDashProfileUpdatesByMemberShareFeed":{"metadata":{"paginationToken":""},"elements":[]}}}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("posts must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Category: RecentActivityCategoryPosts})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func TestGetRecentActivityGraphQLUsesCapturedRawQuery(t *testing.T) {
	tests := []struct {
		category RecentActivityCategory
		queryID  string
	}{
		{category: RecentActivityCategoryPosts, queryID: defaultProfilePostsQueryID},
		{category: RecentActivityCategoryComments, queryID: defaultProfileCommentsQueryID},
		{category: RecentActivityCategoryReactions, queryID: defaultProfileReactionsQueryID},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case recentActivityProfilePath:
					writeProfileURNResponse(t, w, testCapturedProfileURN)
				case recentActivityGraphQLPath:
					assertGraphQLRawQuery(t, r, &graphQLPostsRequest{
						ProfileURN: testCapturedProfileURN,
						QueryID:    tt.queryID,
						Count:      "20",
						Start:      "0",
					})
					writeGraphQLProfileUpdatePage(t, w, collectionForTestCategory(tt.category), testCapturedActivityURN, "")
				case recentActivityUpdatesPath, recentActivityLegacyPath:
					t.Fatalf("%s must not call generic feed endpoint %q", tt.category, r.URL.Path)
				default:
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()

			client := newTestClient(
				WithBaseURL(server.URL),
				WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
			)

			if _, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Category: tt.category}); err != nil {
				t.Fatalf("GetRecentActivity error: %v", err)
			}
		})
	}
}

func TestGetRecentActivityPostsParsesNormalizedGraphQLShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileResponse(t, w)
		case recentActivityGraphQLPath:
			writeJSON(t, w, `{
				"data": {"data": {"feedDashProfileUpdatesByMemberShareFeed": {
					"*elements": ["urn:li:fsd_update:(`+testCapturedActivityURN+`,MEMBER_SHARES,EMPTY,DEFAULT,false)"],
					"metadata": {"paginationToken": ""}
				}}},
				"included": [{
					"$type": "com.linkedin.voyager.dash.feed.Update",
					"entityUrn": "urn:li:fsd_update:(`+testCapturedActivityURN+`,MEMBER_SHARES,EMPTY,DEFAULT,false)",
					"metadata": {"backendUrn": "`+testCapturedActivityURN+`"},
					"commentary": {"text": {"text": "hello normalized"}}
				}]
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("posts must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
		WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{ProfilePostsQueryID: testProfilePostsQueryID}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 10, Category: RecentActivityCategoryPosts})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.URN != testCapturedActivityURN {
		t.Errorf("URN = %q", item.URN)
	}
	if item.RawURN != "urn:li:fsd_update:("+testCapturedActivityURN+",MEMBER_SHARES,EMPTY,DEFAULT,false)" {
		t.Errorf("RawURN = %q", item.RawURN)
	}
	if item.Text != "hello normalized" {
		t.Errorf("Text = %q, want hello normalized", item.Text)
	}
	if item.URL != "https://www.linkedin.com/feed/update/"+testCapturedActivityURN {
		t.Errorf("URL = %q, want activity URL", item.URL)
	}
	if item.ContentCategory != RecentActivityCategoryPosts {
		t.Errorf("ContentCategory = %q, want posts", item.ContentCategory)
	}
}

func TestGetRecentActivityPostsUsesPaginationToken(t *testing.T) {
	graphqlRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileResponse(t, w)
		case recentActivityGraphQLPath:
			graphqlRequests++
			if graphqlRequests == 1 {
				assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{Username: "johndoe", ProfileURN: "urn:li:fsd_profile:abc123", Count: "2", Start: "0"})
				writeJSON(t, w, `{"data":{"feedDashProfileUpdatesByMemberShareFeed":{"metadata":{"paginationToken":"next-token"},"elements":[{"$type":"com.linkedin.voyager.dash.feed.Update","metadata":{"backendUrn":"urn:li:activity:1"},"commentary":{"text":{"text":"first"}}}]}}}`)
				return
			}
			assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{Username: "johndoe", ProfileURN: "urn:li:fsd_profile:abc123", Count: "2", Start: "2", PaginationToken: "next-token"})
			writeJSON(t, w, `{"data":{"feedDashProfileUpdatesByMemberShareFeed":{"metadata":{"paginationToken":""},"elements":[{"$type":"com.linkedin.voyager.dash.feed.Update","metadata":{"backendUrn":"urn:li:activity:2"},"commentary":{"text":{"text":"second"}}}]}}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
		WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{ProfilePostsQueryID: testProfilePostsQueryID}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 2, Category: RecentActivityCategoryPosts})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if graphqlRequests != 2 {
		t.Errorf("graphqlRequests = %d, want 2", graphqlRequests)
	}
}

func TestGetRecentActivityGraphQLPaginationUsesCapturedRawQuery(t *testing.T) {
	graphqlRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileURNResponse(t, w, testCapturedProfileURN)
		case recentActivityGraphQLPath:
			graphqlRequests++
			if graphqlRequests == 1 {
				assertGraphQLRawQuery(t, r, &graphQLPostsRequest{
					ProfileURN: testCapturedProfileURN,
					QueryID:    defaultProfilePostsQueryID,
					Count:      "2",
					Start:      "0",
				})
				writeGraphQLProfileUpdatePage(t, w, "feedDashProfileUpdatesByMemberShareFeed", "urn:li:activity:1", "urn:li:fsd_update:(urn:li:activity:1,MEMBER_SHARES,DEFAULT,false),abc")
				return
			}
			assertGraphQLRawQuery(t, r, &graphQLPostsRequest{
				ProfileURN:      testCapturedProfileURN,
				QueryID:         defaultProfilePostsQueryID,
				Count:           "2",
				Start:           "2",
				PaginationToken: "urn:li:fsd_update:(urn:li:activity:1,MEMBER_SHARES,DEFAULT,false),abc",
			})
			writeGraphQLProfileUpdatePage(t, w, "feedDashProfileUpdatesByMemberShareFeed", "urn:li:activity:2", "")
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("posts must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 2, Category: RecentActivityCategoryPosts})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if graphqlRequests != 2 {
		t.Errorf("graphqlRequests = %d, want 2", graphqlRequests)
	}
}

func TestGetRecentActivityGraphQLNormalizesLegacyProfileURN(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileURNResponse(t, w, testLegacyProfileURN)
		case recentActivityGraphQLPath:
			assertGraphQLRawQuery(t, r, &graphQLPostsRequest{
				ProfileURN: testCapturedProfileURN,
				QueryID:    defaultProfilePostsQueryID,
				Count:      "20",
				Start:      "0",
			})
			writeGraphQLProfileUpdatePage(t, w, "feedDashProfileUpdatesByMemberShareFeed", testCapturedActivityURN, "")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	if _, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Category: RecentActivityCategoryPosts}); err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
}

func TestGetRecentActivityCommentsAndReactionsUseGraphQLEndpoints(t *testing.T) {
	tests := []struct {
		category   RecentActivityCategory
		queryID    string
		collection string
		rawURN     string
	}{
		{
			category:   RecentActivityCategoryComments,
			queryID:    testProfileCommentsQueryID,
			collection: "feedDashProfileUpdatesByMemberComments",
			rawURN:     "urn:li:fsd_update:(" + testCapturedActivityURN + ",PROFILE_COMMENTS,DEBUG_REASON,DEFAULT,false)",
		},
		{
			category:   RecentActivityCategoryReactions,
			queryID:    testProfileReactionsQueryID,
			collection: "feedDashProfileUpdatesByMemberReactions",
			rawURN:     "urn:li:fsd_update:(" + testCapturedActivityURN + ",PROFILE_REACTIONS,DEBUG_REASON,DEFAULT,false)",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case recentActivityProfilePath:
					writeProfileResponse(t, w)
				case recentActivityGraphQLPath:
					assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{
						Username:   "johndoe",
						ProfileURN: "urn:li:fsd_profile:abc123",
						Category:   tt.category,
						QueryID:    tt.queryID,
						Count:      "20",
						Start:      "5",
					})
					writeJSON(t, w, `{
						"data": {"data": {"`+tt.collection+`": {
							"*elements": ["`+tt.rawURN+`"],
							"metadata": {"paginationToken": ""}
						}}},
						"included": [{
							"$type": "com.linkedin.voyager.dash.feed.Update",
							"entityUrn": "`+tt.rawURN+`",
							"metadata": {"backendUrn": "`+testCapturedActivityURN+`"},
							"commentary": {"text": {"text": "captured activity"}},
							"socialDetail": {"totalSocialActivityCounts": {"numLikes": 1, "numComments": 2, "numShares": 3}}
						}]
					}`)
				case recentActivityUpdatesPath, recentActivityLegacyPath:
					t.Fatalf("%s must not call generic feed endpoint %q", tt.category, r.URL.Path)
				default:
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()

			client := newTestClient(
				WithBaseURL(server.URL),
				WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
				WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{
					ProfileCommentsQueryID:  testProfileCommentsQueryID,
					ProfileReactionsQueryID: testProfileReactionsQueryID,
				}),
			)

			items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Start: 5, Category: tt.category})
			if err != nil {
				t.Fatalf("GetRecentActivity error: %v", err)
			}
			if len(items) != 1 {
				t.Fatalf("len(items) = %d, want 1", len(items))
			}
			item := items[0]
			if item.URN != testCapturedActivityURN {
				t.Errorf("URN = %q", item.URN)
			}
			if item.RawURN != tt.rawURN {
				t.Errorf("RawURN = %q, want %q", item.RawURN, tt.rawURN)
			}
			if item.Text != "captured activity" {
				t.Errorf("Text = %q, want captured activity", item.Text)
			}
			if item.ContentCategory != tt.category {
				t.Errorf("ContentCategory = %q, want %q", item.ContentCategory, tt.category)
			}
			if item.CommentText != "" || item.ReactionType != "" {
				t.Errorf("fabricated detail fields: comment=%q reaction=%q", item.CommentText, item.ReactionType)
			}
		})
	}
}

func TestGetRecentActivityCommentsAndReactionsUsePaginationToken(t *testing.T) {
	tests := []struct {
		category   RecentActivityCategory
		queryID    string
		collection string
	}{
		{category: RecentActivityCategoryComments, queryID: testProfileCommentsQueryID, collection: "feedDashProfileUpdatesByMemberComments"},
		{category: RecentActivityCategoryReactions, queryID: testProfileReactionsQueryID, collection: "feedDashProfileUpdatesByMemberReactions"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			graphqlRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case recentActivityProfilePath:
					writeProfileResponse(t, w)
				case recentActivityGraphQLPath:
					graphqlRequests++
					if graphqlRequests == 1 {
						assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{Username: "johndoe", ProfileURN: "urn:li:fsd_profile:abc123", Category: tt.category, QueryID: tt.queryID, Count: "2", Start: "0"})
						writeGraphQLProfileUpdatePage(t, w, tt.collection, "urn:li:activity:1", "next-token")
						return
					}
					assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{Username: "johndoe", ProfileURN: "urn:li:fsd_profile:abc123", Category: tt.category, QueryID: tt.queryID, Count: "2", Start: "2", PaginationToken: "next-token"})
					writeGraphQLProfileUpdatePage(t, w, tt.collection, "urn:li:activity:2", "")
				case recentActivityUpdatesPath, recentActivityLegacyPath:
					t.Fatalf("%s must not call generic feed endpoint %q", tt.category, r.URL.Path)
				default:
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()

			client := newTestClient(
				WithBaseURL(server.URL),
				WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
				WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{
					ProfileCommentsQueryID:  testProfileCommentsQueryID,
					ProfileReactionsQueryID: testProfileReactionsQueryID,
				}),
			)

			items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 2, Category: tt.category})
			if err != nil {
				t.Fatalf("GetRecentActivity error: %v", err)
			}
			if len(items) != 2 {
				t.Fatalf("len(items) = %d, want 2", len(items))
			}
			if graphqlRequests != 2 {
				t.Errorf("graphqlRequests = %d, want 2", graphqlRequests)
			}
		})
	}
}

func TestGetRecentActivityGraphQLCommentUsesSiblingCommentEntity(t *testing.T) {
	const (
		wrapperURN    = "urn:li:fsd_update:(urn:li:activity:7475170315271254017,PROFILE_COMMENTS,DEBUG_REASON,DEFAULT,false)"
		commentURN    = "urn:li:comment:(urn:li:activity:7474802230450368514,7475170315271254017)"
		parentURN     = "urn:li:activity:7474802230450368514"
		parentText    = "OpenAI's Codex is destroying SSDs one of activity"
		commentText   = "How is this data used? Who do they collect it for?"
		commentActor  = "urn:li:fsd_profile:comment-author"
		commentAuthor = "Nikita Benkovich"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileResponse(t, w)
		case recentActivityGraphQLPath:
			assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{
				Username:   "johndoe",
				ProfileURN: "urn:li:fsd_profile:abc123",
				Category:   RecentActivityCategoryComments,
				QueryID:    testProfileCommentsQueryID,
				Count:      "20",
				Start:      "0",
			})
			writeJSON(t, w, `{
				"data": {"data": {"feedDashProfileUpdatesByMemberComments": {
					"*elements": ["`+wrapperURN+`"],
					"metadata": {"paginationToken": ""}
				}}},
				"included": [{
					"$type": "com.linkedin.voyager.dash.feed.Update",
					"entityUrn": "`+wrapperURN+`",
					"metadata": {"backendUrn": "urn:li:activity:7475170315271254017"},
					"commentary": {"text": {"text": "`+parentText+`"}},
					"socialContent": {"shareUrl": "https://www.linkedin.com/posts/example"},
					"socialDetail": {"totalSocialActivityCounts": {"numLikes": 1, "numComments": 2, "numShares": 3}}
				}, {
					"$type": "com.linkedin.voyager.feed.CommentUpdate",
					"entityUrn": "`+commentURN+`",
					"actor": {"urn": "`+commentActor+`", "name": {"text": "`+commentAuthor+`"}},
					"commentary": {"text": {"text": "`+commentText+`"}}
				}]
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("comments must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
		WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{ProfileCommentsQueryID: testProfileCommentsQueryID}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Category: RecentActivityCategoryComments})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.URN != commentURN || item.CommentURN != commentURN {
		t.Errorf("comment URN = %q/%q, want %q", item.URN, item.CommentURN, commentURN)
	}
	if item.RawURN != wrapperURN {
		t.Errorf("RawURN = %q, want %q", item.RawURN, wrapperURN)
	}
	if item.Text != commentText || item.CommentText != commentText {
		t.Errorf("comment text = %q/%q, want %q", item.Text, item.CommentText, commentText)
	}
	if item.CommentedOnText != parentText {
		t.Errorf("CommentedOnText = %q, want %q", item.CommentedOnText, parentText)
	}
	if item.CommentedOnURN != parentURN {
		t.Errorf("CommentedOnURN = %q, want %q", item.CommentedOnURN, parentURN)
	}
	if item.CommentedOnURL != "https://www.linkedin.com/feed/update/"+parentURN || item.URL != item.CommentedOnURL {
		t.Errorf("commented-on URL = %q/%q", item.CommentedOnURL, item.URL)
	}
	if item.CommentActorURN != commentActor || item.ActorURN != commentActor {
		t.Errorf("comment actor URN = %q/%q", item.CommentActorURN, item.ActorURN)
	}
	if item.CommentActorName != commentAuthor || item.ActorName != commentAuthor {
		t.Errorf("comment actor name = %q/%q", item.CommentActorName, item.ActorName)
	}
	if item.ContentCategory != RecentActivityCategoryComments {
		t.Errorf("ContentCategory = %q, want comments", item.ContentCategory)
	}
}

func TestGetRecentActivityGraphQLCommentNormalizesActivityParentURN(t *testing.T) {
	const (
		wrapperURN   = "urn:li:fsd_update:(urn:li:activity:7475170315271254017,PROFILE_COMMENTS,DEBUG_REASON,DEFAULT,false)"
		commentURN   = "urn:li:comment:(activity:7474802230450368514,7475170315271254017)"
		parentURN    = "urn:li:activity:7474802230450368514"
		parentText   = "Parent update text"
		commentText  = "How is this data used? Who do they collect it for?"
		commentActor = "urn:li:fsd_profile:comment-author"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeProfileResponse(t, w)
		case recentActivityGraphQLPath:
			writeJSON(t, w, `{
				"data": {"data": {"feedDashProfileUpdatesByMemberComments": {
					"*elements": ["`+wrapperURN+`"],
					"metadata": {"paginationToken": ""}
				}}},
				"included": [{
					"$type": "com.linkedin.voyager.dash.feed.Update",
					"entityUrn": "`+wrapperURN+`",
					"metadata": {"backendUrn": "urn:li:activity:7475170315271254017"},
					"commentary": {"text": {"text": "`+parentText+`"}}
				}, {
					"$type": "com.linkedin.voyager.feed.CommentUpdate",
					"entityUrn": "`+commentURN+`",
					"actor": {"urn": "`+commentActor+`"},
					"commentary": {"text": {"text": "`+commentText+`"}}
				}]
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("comments must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
		WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{ProfileCommentsQueryID: testProfileCommentsQueryID}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 20, Category: RecentActivityCategoryComments})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.URN != commentURN || item.CommentURN != commentURN {
		t.Errorf("comment URN = %q/%q, want %q", item.URN, item.CommentURN, commentURN)
	}
	if item.Text != commentText || item.CommentText != commentText {
		t.Errorf("comment text = %q/%q, want %q", item.Text, item.CommentText, commentText)
	}
	if item.CommentedOnURN != parentURN {
		t.Errorf("CommentedOnURN = %q, want %q", item.CommentedOnURN, parentURN)
	}
	if item.CommentedOnURL != "https://www.linkedin.com/feed/update/"+parentURN {
		t.Errorf("CommentedOnURL = %q", item.CommentedOnURL)
	}
	if item.CommentedOnText != parentText {
		t.Errorf("CommentedOnText = %q, want %q", item.CommentedOnText, parentText)
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
			if r.Header.Get("Referer") != "https://www.linkedin.com/in/johndoe/recent-activity/all/" {
				t.Errorf("Referer = %q, want all activity URL", r.Header.Get("Referer"))
			}
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
				"data": {"elements": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:1",
					"createdAt": 2000,
					"actor": {"urn": "urn:li:member:1", "name": {"text": "John Doe"}},
					"commentary": {"text": {"text": "hello activity"}},
					"socialDetail": {"likes": 3, "comments": 4, "shares": 5}
				}]}
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

func TestGetRecentActivityDefaultCategoryUsesAllRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath:
			query := r.URL.Query()
			if query.Get("q") != "memberShareFeed" {
				t.Errorf("q = %q, want memberShareFeed", query.Get("q"))
			}
			if query.Get("profileUrn") != "urn:li:fsd_profile:abc123" {
				t.Errorf("profileUrn = %q, want urn:li:fsd_profile:abc123", query.Get("profileUrn"))
			}
			if query.Get("count") != "10" {
				t.Errorf("count = %q, want 10", query.Get("count"))
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

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("len(items) = %d, want 0", len(items))
	}
}

func TestGetRecentActivityCategoryOverfetchesAndFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath:
			query := r.URL.Query()
			if r.Header.Get("Referer") != "https://www.linkedin.com/in/johndoe/recent-activity/images/" {
				t.Errorf("Referer = %q, want images activity URL", r.Header.Get("Referer"))
			}
			if query.Get("count") != "15" {
				t.Errorf("count = %q, want 15", query.Get("count"))
			}
			writeJSON(t, w, `{
				"data": {"elements": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:1",
					"content": {"image": {"attributes": []}}
				}, {
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:2",
					"commentary": {"text": {"text": "plain text"}}
				}]}
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

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 3, Category: RecentActivityCategoryImages, ExperimentalLocalFilter: true})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].URN != testActivityURN1 {
		t.Errorf("URN = %q, want %s", items[0].URN, testActivityURN1)
	}
	if items[0].ContentCategory != RecentActivityCategoryImages {
		t.Errorf("ContentCategory = %q, want images", items[0].ContentCategory)
	}
}

func TestGetRecentActivityPostsRefererAndFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath:
			if r.Header.Get("Referer") != "https://www.linkedin.com/in/johndoe/recent-activity/posts/" {
				t.Errorf("Referer = %q, want posts activity URL", r.Header.Get("Referer"))
			}
			writeJSON(t, w, `{
				"data": {"elements": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:1",
					"commentary": {"text": {"text": "plain text"}}
				}, {
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:2",
					"content": {"image": {"rootUrl": "https://example.test/image.jpg"}}
				}, {
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:3",
					"reactionType": "PRAISE"
				}, {
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:4",
					"commentUrn": "urn:li:comment:(urn:li:activity:1,123)"
				}]}
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

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 10, Category: RecentActivityCategoryPosts, ExperimentalLocalFilter: true})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].URN != testActivityURN1 {
		t.Errorf("URN = %q, want %s", items[0].URN, testActivityURN1)
	}
}

func TestGetRecentActivityFilteredPaginationFindsLaterPagePosts(t *testing.T) {
	activityStarts := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath:
			activityStarts = append(activityStarts, r.URL.Query().Get("start"))
			switch r.URL.Query().Get("start") {
			case "0":
				writeJSON(t, w, `{
					"data": {"elements": [{
						"$type": "com.linkedin.voyager.feed.Update",
						"entityUrn": "urn:li:activity:1",
						"content": {"image": {"rootUrl": "https://example.test/one.jpg"}}
					}, {
						"$type": "com.linkedin.voyager.feed.Update",
						"entityUrn": "urn:li:activity:2",
						"content": {"image": {"rootUrl": "https://example.test/two.jpg"}}
					}, {
						"$type": "com.linkedin.voyager.feed.Update",
						"entityUrn": "urn:li:activity:3",
						"content": {"image": {"rootUrl": "https://example.test/three.jpg"}}
					}, {
						"$type": "com.linkedin.voyager.feed.Update",
						"entityUrn": "urn:li:activity:4",
						"content": {"image": {"rootUrl": "https://example.test/four.jpg"}}
					}, {
						"$type": "com.linkedin.voyager.feed.Update",
						"entityUrn": "urn:li:activity:5",
						"content": {"image": {"rootUrl": "https://example.test/five.jpg"}}
					}]}
				}`)
			case "5":
				writeJSON(t, w, `{
					"data": {"elements": [{
						"$type": "com.linkedin.voyager.feed.Update",
						"entityUrn": "urn:li:activity:6",
						"actor": {"urn": "urn:li:member:1"},
						"commentary": {"text": {"text": "later page post"}}
					}]}
				}`)
			default:
				t.Fatalf("unexpected start %q", r.URL.Query().Get("start"))
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}),
	)

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 1, Category: RecentActivityCategoryPosts, ExperimentalLocalFilter: true})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].URN != "urn:li:activity:6" {
		t.Errorf("URN = %q, want later-page post", items[0].URN)
	}
	if len(activityStarts) != 2 || activityStarts[0] != "0" || activityStarts[1] != "5" {
		t.Errorf("activityStarts = %v, want [0 5]", activityStarts)
	}
}

func TestGetRecentActivityNoFallbackAfterNoFilteredMatches(t *testing.T) {
	activityPaths := make([]string, 0, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "John"}]
			}`)
		case recentActivityUpdatesPath:
			activityPaths = append(activityPaths, r.URL.Path)
			writeJSON(t, w, `{
				"data": {"elements": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:1",
					"content": {"image": {"rootUrl": "https://example.test/image.jpg"}}
				}]}
			}`)
		case recentActivityLegacyPath:
			activityPaths = append(activityPaths, r.URL.Path)
			writeJSON(t, w, `{
				"data": {"elements": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:2",
					"actor": {"urn": "urn:li:member:1"},
					"commentary": {"text": {"text": "legacy post"}}
				}]}
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

	items, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Limit: 5, Category: RecentActivityCategoryPosts, ExperimentalLocalFilter: true})
	if err != nil {
		t.Fatalf("GetRecentActivity error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
	if len(activityPaths) != 1 || activityPaths[0] != recentActivityUpdatesPath {
		t.Errorf("activityPaths = %v, want primary only", activityPaths)
	}
}

func TestGetRecentActivityVideosUnsupportedWithoutExperimentalLocalFilter(t *testing.T) {
	client := newTestClient(WithCredentials(&Credentials{LiAt: "token", JSessID: "session"}))

	_, err := client.GetRecentActivity(context.Background(), "johndoe", &RecentActivityOptions{Category: RecentActivityCategoryVideos})
	if err == nil {
		t.Fatal("expected error")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeUnsupported {
		t.Errorf("code = %q, want %q", apiErr.Code, ErrCodeUnsupported)
	}
}

func TestGetRecentActivityDebugShapeRedactsRawContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "Secret Name"}]
			}`)
		case recentActivityUpdatesPath:
			writeJSON(t, w, `{
				"data": {"elements": [{
					"$type": "com.linkedin.voyager.feed.Update",
					"entityUrn": "urn:li:activity:1",
					"commentary": {"text": {"text": "private post body"}}
				}]},
				"included": [{
					"$type": "com.linkedin.voyager.feed.Actor",
					"name": "Private Person"
				}],
				"paging": {"count": 10, "start": 0, "links": [{"rel": "next", "href": "secret-next"}]}
			}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session", CSRFToken: "csrf"}),
	)

	shape, err := client.GetRecentActivityDebugShape(context.Background(), "johndoe", &RecentActivityOptions{Limit: 10, Category: RecentActivityCategoryAll})
	if err != nil {
		t.Fatalf("GetRecentActivityDebugShape error: %v", err)
	}
	if shape.EndpointPath != recentActivityUpdatesPath {
		t.Errorf("EndpointPath = %q, want %q", shape.EndpointPath, recentActivityUpdatesPath)
	}
	if shape.Status != http.StatusOK {
		t.Errorf("Status = %d, want 200", shape.Status)
	}
	if shape.DataCount != 1 || shape.IncludedCount != 1 {
		t.Errorf("counts = data %d included %d, want 1/1", shape.DataCount, shape.IncludedCount)
	}
	if !shape.HasNextLink {
		t.Error("HasNextLink = false, want true")
	}
	shapeJSON, err := json.Marshal(shape)
	if err != nil {
		t.Fatalf("Marshal shape: %v", err)
	}
	output := string(shapeJSON)
	for _, secret := range []string{"token", "session", "csrf", "Cookie", "li_at", "JSESSIONID", "private post body", "Private Person", "Secret Name", "secret-next"} {
		if strings.Contains(output, secret) {
			t.Errorf("debug shape leaked %q: %s", secret, output)
		}
	}
	if !strings.Contains(output, "com.linkedin.voyager.feed.Update") {
		t.Errorf("debug shape missing example type: %s", output)
	}
}

func TestGetRecentActivityDebugShapePostsTargetsGraphQLAndRedactsRawContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case recentActivityProfilePath:
			writeJSON(t, w, `{
				"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
				"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "Secret Name"}]
			}`)
		case recentActivityGraphQLPath:
			assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{Username: "johndoe", ProfileURN: "urn:li:fsd_profile:abc123", Count: "10", Start: "0"})
			writeJSON(t, w, `{
				"data": {"feedDashProfileUpdatesByMemberShareFeed": {"metadata": {"paginationToken": "secret-next-token"}, "elements": [{
					"$type": "com.linkedin.voyager.dash.feed.Update",
					"metadata": {"backendUrn": "urn:li:activity:1"},
					"commentary": {"text": {"text": "private post body"}}
				}]}},
				"included": [{"$type": "com.linkedin.voyager.dash.feed.Actor", "name": "Private Person"}]
			}`)
		case recentActivityUpdatesPath, recentActivityLegacyPath:
			t.Fatalf("posts debug shape must not call generic feed endpoint %q", r.URL.Path)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "token", JSessID: "session", CSRFToken: "csrf"}),
		WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{ProfilePostsQueryID: testProfilePostsQueryID}),
	)

	shape, err := client.GetRecentActivityDebugShape(context.Background(), "johndoe", &RecentActivityOptions{Limit: 10, Category: RecentActivityCategoryPosts})
	if err != nil {
		t.Fatalf("GetRecentActivityDebugShape error: %v", err)
	}
	if shape.EndpointPath != recentActivityGraphQLPath {
		t.Errorf("EndpointPath = %q, want %q", shape.EndpointPath, recentActivityGraphQLPath)
	}
	if shape.Status != http.StatusOK {
		t.Errorf("Status = %d, want 200", shape.Status)
	}
	shapeJSON, err := json.Marshal(shape)
	if err != nil {
		t.Fatalf("Marshal shape: %v", err)
	}
	output := string(shapeJSON)
	for _, secret := range []string{"token", "session", "csrf", "Cookie", "li_at", "JSESSIONID", "private post body", "Private Person", "Secret Name", "secret-next-token"} {
		if strings.Contains(output, secret) {
			t.Errorf("debug shape leaked %q: %s", secret, output)
		}
	}
	for _, want := range []string{"includeWebMetadata=true", "queryId=" + testProfilePostsQueryID, "com.linkedin.voyager.dash.feed.Update"} {
		if !strings.Contains(output, want) {
			t.Errorf("debug shape missing %q: %s", want, output)
		}
	}
}

func TestGetRecentActivityDebugShapeCommentsAndReactionsTargetsGraphQL(t *testing.T) {
	tests := []struct {
		category   RecentActivityCategory
		queryID    string
		collection string
	}{
		{category: RecentActivityCategoryComments, queryID: testProfileCommentsQueryID, collection: "feedDashProfileUpdatesByMemberComments"},
		{category: RecentActivityCategoryReactions, queryID: testProfileReactionsQueryID, collection: "feedDashProfileUpdatesByMemberReactions"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case recentActivityProfilePath:
					writeJSON(t, w, `{
						"data": {"*elements": ["urn:li:fsd_profile:abc123"]},
						"included": [{"entityUrn": "urn:li:fsd_profile:abc123", "firstName": "Secret Name"}]
					}`)
				case recentActivityGraphQLPath:
					assertGraphQLPostsRequest(t, r, &graphQLPostsRequest{Username: "johndoe", ProfileURN: "urn:li:fsd_profile:abc123", Category: tt.category, QueryID: tt.queryID, Count: "10", Start: "0"})
					writeJSON(t, w, `{
						"data": {"data": {"`+tt.collection+`": {
							"metadata": {"paginationToken": "secret-next-token"},
							"*elements": ["urn:li:fsd_update:(urn:li:activity:1,PROFILE,DEBUG,DEFAULT,false)"]
						}}},
						"included": [{
							"$type": "com.linkedin.voyager.dash.feed.Update",
							"entityUrn": "urn:li:fsd_update:(urn:li:activity:1,PROFILE,DEBUG,DEFAULT,false)",
							"metadata": {"backendUrn": "urn:li:activity:1"},
							"commentary": {"text": {"text": "private body"}},
							"actor": {"name": {"text": "Private Person"}}
						}]
					}`)
				case recentActivityUpdatesPath, recentActivityLegacyPath:
					t.Fatalf("%s debug shape must not call generic feed endpoint %q", tt.category, r.URL.Path)
				default:
					t.Fatalf("unexpected path %q", r.URL.Path)
				}
			}))
			defer server.Close()

			client := newTestClient(
				WithBaseURL(server.URL),
				WithCredentials(&Credentials{LiAt: "token", JSessID: "session", CSRFToken: "csrf"}),
				WithRecentActivityGraphQLConfig(RecentActivityGraphQLConfig{
					ProfileCommentsQueryID:  testProfileCommentsQueryID,
					ProfileReactionsQueryID: testProfileReactionsQueryID,
				}),
			)

			shape, err := client.GetRecentActivityDebugShape(context.Background(), "johndoe", &RecentActivityOptions{Limit: 10, Category: tt.category})
			if err != nil {
				t.Fatalf("GetRecentActivityDebugShape error: %v", err)
			}
			if shape.EndpointPath != recentActivityGraphQLPath {
				t.Errorf("EndpointPath = %q, want %q", shape.EndpointPath, recentActivityGraphQLPath)
			}
			if shape.DataCount != 1 || shape.IncludedCount != 1 {
				t.Errorf("counts = data %d included %d, want 1/1", shape.DataCount, shape.IncludedCount)
			}
			shapeJSON, err := json.Marshal(shape)
			if err != nil {
				t.Fatalf("Marshal shape: %v", err)
			}
			output := string(shapeJSON)
			for _, secret := range []string{"token", "session", "csrf", "Cookie", "li_at", "JSESSIONID", "private body", "Private Person", "Secret Name", "secret-next-token"} {
				if strings.Contains(output, secret) {
					t.Errorf("debug shape leaked %q: %s", secret, output)
				}
			}
			for _, want := range []string{"includeWebMetadata=true", "queryId=" + tt.queryID, "com.linkedin.voyager.dash.feed.Update"} {
				if !strings.Contains(output, want) {
					t.Errorf("debug shape missing %q: %s", want, output)
				}
			}
		})
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
				"data": {"elements": [{"$type": "com.linkedin.voyager.feed.Update", "createdAt": 1000}]}
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
				"data": {"elements": [{"$type": "com.linkedin.voyager.feed.Update", "createdAt": 1000}]}
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
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"createdAt": 1000,
				"actor": {"urn": "urn:li:member:1", "name": {"text": "John Doe"}},
				"commentaryV2": {"text": "First post"}
			}]
		}`),
		Included: []json.RawMessage{
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
	if items[0].URN != testActivityURN2 {
		t.Errorf("first URN = %q, want %s", items[0].URN, testActivityURN2)
	}
	if items[0].Text != "Second post" {
		t.Errorf("Text = %q, want Second post", items[0].Text)
	}
	if items[0].LikeCount != 3 || items[0].CommentCount != 4 || items[0].ShareCount != 5 {
		t.Errorf("counts = %d/%d/%d, want 3/4/5", items[0].LikeCount, items[0].CommentCount, items[0].ShareCount)
	}
}

func TestParseRecentActivityFromReferencedIncludedWrappers(t *testing.T) {
	const wrapperURN = "urn:li:fs_feedUpdate:(V2&MEMBER_SHARES,urn:li:activity:7475116029644414976)"
	resp := &VoyagerResponse{
		Data: []byte(`{
			"*elements": ["` + wrapperURN + `"]
		}`),
		Included: []json.RawMessage{
			[]byte(`{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "` + wrapperURN + `",
				"createdAt": 2000,
				"actor": {"urn": "` + testMemberURN + `", "name": {"text": "Jane Smith"}},
				"commentary": {"text": {"text": "Referenced wrapper post"}},
				"socialActivityCounts": {"numLikes": 7, "numComments": 8, "numShares": 9}
			}`),
		},
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.URN != testActivityURN {
		t.Errorf("URN = %q, want normalized wrapper activity URN", item.URN)
	}
	if item.RawURN != wrapperURN {
		t.Errorf("RawURN = %q, want %q", item.RawURN, wrapperURN)
	}
	if item.Text != "Referenced wrapper post" {
		t.Errorf("Text = %q, want referenced wrapper post", item.Text)
	}
	if item.ActorURN != testMemberURN || item.ActorName != "Jane Smith" {
		t.Errorf("actor = %q/%q, want referenced wrapper actor", item.ActorURN, item.ActorName)
	}
	if item.LikeCount != 7 || item.CommentCount != 8 || item.ShareCount != 9 {
		t.Errorf("counts = %d/%d/%d, want 7/8/9", item.LikeCount, item.CommentCount, item.ShareCount)
	}
	if item.URL != "https://www.linkedin.com/feed/update/"+testActivityURN {
		t.Errorf("URL = %q, want normalized activity URL", item.URL)
	}
}

func TestParseRecentActivityAllDoesNotEmitIncludedComments(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"createdAt": 2000,
				"actor": {"urn": "urn:li:member:1"},
				"commentary": {"text": {"text": "Primary post"}}
			}]
		}`),
		Included: []json.RawMessage{
			[]byte(`{
				"$type": "com.linkedin.voyager.feed.CommentUpdate",
				"entityUrn": "` + testCommentURN + `",
				"createdAt": 3000,
				"actor": {"urn": "` + testMemberURN + `"},
				"message": {"text": "Included lookup comment"}
			}`),
		},
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].URN != testActivityURN1 {
		t.Errorf("URN = %q, want primary activity", items[0].URN)
	}
}

func TestParseRecentActivityEmptyElementsDoNotEmitIncludedComments(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{"elements": []}`),
		Included: []json.RawMessage{
			[]byte(`{
				"$type": "com.linkedin.voyager.feed.CommentUpdate",
				"entityUrn": "` + testCommentURN + `",
				"createdAt": 3000,
				"actor": {"urn": "` + testMemberURN + `"},
				"message": {"text": "Included lookup comment"}
			}`),
		},
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}
}

func TestParseRecentActivityNormalizesWrapperURN(t *testing.T) {
	const rawURN = `urn:li:fs_feedUpdate:(V2\u0026MEMBER_SHARES,urn:li:activity:7475116029644414976)`
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:fs_feedUpdate:(V2\\u0026MEMBER_SHARES,urn:li:activity:7475116029644414976)"
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.URN != testActivityURN {
		t.Errorf("URN = %q, want normalized activity URN", item.URN)
	}
	if item.RawURN != rawURN {
		t.Errorf("RawURN = %q, want %q", item.RawURN, rawURN)
	}
	if item.URL != "https://www.linkedin.com/feed/update/"+testActivityURN {
		t.Errorf("URL = %q, want activity URL", item.URL)
	}
	if item.Text != "" || item.ActorName != "" || !item.CreatedAt.IsZero() {
		t.Errorf("fabricated fields: text=%q actor=%q createdAt=%s", item.Text, item.ActorName, item.CreatedAt)
	}
}

func TestParseRecentActivityNormalizesAmpersandWrapperURN(t *testing.T) {
	const rawURN = "urn:li:fs_feedUpdate:(V2&MEMBER_SHARES,urn:li:activity:7475116029644414976)"
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "` + rawURN + `"
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].URN != testActivityURN {
		t.Errorf("URN = %q, want normalized activity URN", items[0].URN)
	}
	if items[0].RawURN != rawURN {
		t.Errorf("RawURN = %q, want %q", items[0].RawURN, rawURN)
	}
}

func TestParseRecentActivityMergesIncludedEntityFields(t *testing.T) {
	const rawURN = "urn:li:fs_feedUpdate:(V2&MEMBER_SHARES,urn:li:activity:7475116029644414976)"
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "` + rawURN + `"
			}]
		}`),
		Included: []json.RawMessage{
			[]byte(`{
				"$type": "com.linkedin.voyager.feed.ShareUpdate",
				"entityUrn": "urn:li:activity:7475116029644414976",
				"createdAt": 1780000000000,
				"actor": {"urn": "` + testMemberURN + `", "name": {"text": "Jane Smith"}},
				"commentary": {"text": {"text": "Included entity text"}},
				"socialActivityCounts": {"numLikes": 7, "numComments": 8, "numShares": 9}
			}`),
		},
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}

	item := items[0]
	if item.URN != testActivityURN {
		t.Errorf("URN = %q, want normalized activity URN", item.URN)
	}
	if item.RawURN != rawURN {
		t.Errorf("RawURN = %q, want %q", item.RawURN, rawURN)
	}
	if item.Text != "Included entity text" {
		t.Errorf("Text = %q, want included entity text", item.Text)
	}
	if item.ActorURN != testMemberURN || item.ActorName != "Jane Smith" {
		t.Errorf("actor = %q/%q, want included actor", item.ActorURN, item.ActorName)
	}
	if item.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want included timestamp")
	}
	if item.LikeCount != 7 || item.CommentCount != 8 || item.ShareCount != 9 {
		t.Errorf("counts = %d/%d/%d, want 7/8/9", item.LikeCount, item.CommentCount, item.ShareCount)
	}
}

func TestParseRecentActivityClassifiesContentCategories(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"content": {"image": {"rootUrl": "https://example.test/image.jpg"}}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:2",
				"content": {"media": {"mediaType": "VIDEO"}}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:3",
				"content": {"document": {"urn": "urn:li:document:123"}}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:4",
				"content": {"entity": "urn:li:event:123"}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:5",
				"socialActivityCounts": {"numLikes": 99},
				"commentary": {"text": {"text": "plain update"}}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}

	categories := map[string]RecentActivityCategory{}
	for i := range items {
		categories[items[i].URN] = items[i].ContentCategory
	}

	wants := map[string]RecentActivityCategory{
		testActivityURN1:    RecentActivityCategoryImages,
		testActivityURN2:    RecentActivityCategoryVideos,
		"urn:li:activity:3": RecentActivityCategoryDocuments,
		"urn:li:activity:4": RecentActivityCategoryEvents,
		"urn:li:activity:5": "",
	}
	for urn, want := range wants {
		if categories[urn] != want {
			t.Errorf("%s category = %q, want %q", urn, categories[urn], want)
		}
	}
	if filtered := filterRecentActivityByCategory(items, RecentActivityCategoryImages); len(filtered) != 1 || filtered[0].URN != testActivityURN1 {
		t.Errorf("images filter = %#v, want activity 1", filtered)
	}
	if filtered := filterRecentActivityByCategory(items, RecentActivityCategoryVideos); len(filtered) != 1 || filtered[0].URN != testActivityURN2 {
		t.Errorf("videos filter = %#v, want activity 2", filtered)
	}
	if filtered := filterRecentActivityByCategory(items, RecentActivityCategoryDocuments); len(filtered) != 1 || filtered[0].URN != "urn:li:activity:3" {
		t.Errorf("documents filter = %#v, want activity 3", filtered)
	}
	if filtered := filterRecentActivityByCategory(items, RecentActivityCategoryEvents); len(filtered) != 1 || filtered[0].URN != "urn:li:activity:4" {
		t.Errorf("events filter = %#v, want activity 4", filtered)
	}
}

func TestParseRecentActivityPostsFilter(t *testing.T) {
	items := []ActivityItem{
		{URN: testActivityURN1, Type: "com.linkedin.voyager.feed.Update", Text: "plain post"},
		{URN: testActivityURN2, ContentCategory: RecentActivityCategoryImages},
		{URN: "urn:li:activity:3", ContentCategory: RecentActivityCategoryVideos},
		{URN: "urn:li:activity:4", ContentCategory: RecentActivityCategoryDocuments},
		{URN: "urn:li:activity:5", ContentCategory: RecentActivityCategoryEvents},
		{URN: "urn:li:activity:6", ContentCategory: RecentActivityCategoryReactions},
		{URN: "urn:li:activity:7", ContentCategory: RecentActivityCategoryComments},
	}

	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryPosts)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].URN != testActivityURN1 {
		t.Errorf("URN = %q, want %s", filtered[0].URN, testActivityURN1)
	}
}

func TestParseRecentActivityPostsExcludeWrapperOnlyArtifacts(t *testing.T) {
	items := []ActivityItem{
		{URN: testActivityURN1, Type: "com.linkedin.voyager.feed.Update", RawURN: "urn:li:fs_feedUpdate:(V2&MEMBER_SHARES,urn:li:activity:1)"},
		{URN: testActivityURN2, Type: "com.linkedin.voyager.feed.Update", RawURN: testActivityURN2},
	}

	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryPosts)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].URN != testActivityURN2 {
		t.Errorf("URN = %q, want %s", filtered[0].URN, testActivityURN2)
	}
}

func TestParseRecentActivityPostsKeepMergedWrapperWithIncompleteFields(t *testing.T) {
	items := []ActivityItem{
		{URN: testActivityURN1, Type: "com.linkedin.voyager.feed.Update", RawURN: "urn:li:fs_feedUpdate:(V2&MEMBER_SHARES,urn:li:activity:1)", hasLookupDetails: true},
	}

	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryPosts)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].URN != testActivityURN1 {
		t.Errorf("URN = %q, want %s", filtered[0].URN, testActivityURN1)
	}
}

func TestParseRecentActivityAllUnfiltered(t *testing.T) {
	items := []ActivityItem{
		{URN: testActivityURN1},
		{URN: testActivityURN2, ContentCategory: RecentActivityCategoryImages},
		{URN: "urn:li:activity:3", ContentCategory: RecentActivityCategoryComments},
	}

	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryAll)
	if len(filtered) != len(items) {
		t.Fatalf("len(filtered) = %d, want %d", len(filtered), len(items))
	}
}

func TestParseRecentActivityClassifiesReactionsOnlyFromReactionSignals(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"reactionType": "LIKE"
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:2",
				"reaction": "urn:li:reaction:(` + testMemberURN + `,urn:li:activity:2)"
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:3",
				"socialActivityCounts": {"numLikes": 99}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryReactions)
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	if filtered[0].URN != testActivityURN1 {
		t.Errorf("URN = %q, want %s", filtered[0].URN, testActivityURN1)
	}
	if filtered[1].URN != testActivityURN2 {
		t.Errorf("URN = %q, want %s", filtered[1].URN, testActivityURN2)
	}
}

func TestParseRecentActivityDoesNotClassifyBroadReactionText(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"commentary": {"text": {"text": "I reacted to a product update"}}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:2",
				"tracking": {"action": "com.linkedin.feed.reactionTracking"}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:3",
				"label": "Reacted by someone else"
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryReactions)
	if len(filtered) != 0 {
		t.Fatalf("len(filtered) = %d, want 0: %#v", len(filtered), filtered)
	}
}

func TestParseRecentActivityReactionDetails(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"reactionType": "PRAISE",
				"reactionUrn": "urn:li:reaction:(` + testMemberURN + `,` + testReactedToURN + `)"
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.ReactionType != "PRAISE" {
		t.Errorf("ReactionType = %q, want PRAISE", item.ReactionType)
	}
	if item.ReactionURN != "urn:li:reaction:("+testMemberURN+","+testReactedToURN+")" {
		t.Errorf("ReactionURN = %q", item.ReactionURN)
	}
	if item.ReactionActorURN != testMemberURN {
		t.Errorf("ReactionActorURN = %q, want %s", item.ReactionActorURN, testMemberURN)
	}
	if item.ReactedToURN != testReactedToURN {
		t.Errorf("ReactedToURN = %q, want %s", item.ReactedToURN, testReactedToURN)
	}
	if item.ReactedToURL != testReactedToURL {
		t.Errorf("ReactedToURL = %q", item.ReactedToURL)
	}
}

func TestParseRecentActivityCommentDetails(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.CommentUpdate",
				"entityUrn": "urn:li:activity:1",
				"reactionType": "LIKE",
				"comment": {
					"entityUrn": "` + testCommentURN + `",
					"actor": {"urn": "` + testMemberURN + `", "name": {"text": "` + testCommentActorName + `"}},
					"message": {"text": "` + testCommentText + `"}
				}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.ContentCategory != RecentActivityCategoryComments {
		t.Errorf("ContentCategory = %q, want comments", item.ContentCategory)
	}
	if item.CommentURN != testCommentURN {
		t.Errorf("CommentURN = %q", item.CommentURN)
	}
	if item.CommentActorURN != testMemberURN || item.ActorURN != testMemberURN {
		t.Errorf("comment actor URN = %q/%q", item.CommentActorURN, item.ActorURN)
	}
	if item.CommentActorName != testCommentActorName || item.ActorName != testCommentActorName {
		t.Errorf("comment actor name = %q/%q", item.CommentActorName, item.ActorName)
	}
	if item.CommentText != testCommentText || item.Text != testCommentText {
		t.Errorf("comment text = %q/%q", item.CommentText, item.Text)
	}
	if item.CommentedOnURN != testCommentedOnURN {
		t.Errorf("CommentedOnURN = %q, want urn:li:activity:998", item.CommentedOnURN)
	}
	if item.CommentedOnURL != testCommentedOnURL {
		t.Errorf("CommentedOnURL = %q", item.CommentedOnURL)
	}
	if item.ReactionType != "" || item.ReactionURN != "" || item.ReactedToURN != "" {
		t.Errorf("reaction fields not cleared: %#v", item)
	}
}

func TestParseRecentActivityTopLevelCommentEntityDetails(t *testing.T) {
	commentURN := testCommentURN
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.CommentUpdate",
				"entityUrn": "` + commentURN + `",
				"createdAt": 2000,
				"actor": {"urn": "` + testMemberURN + `", "name": {"text": "` + testCommentActorName + `"}},
				"message": {"text": "` + testCommentText + `"}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.URN != commentURN {
		t.Errorf("URN = %q, want comment URN", item.URN)
	}
	if item.CommentURN != commentURN {
		t.Errorf("CommentURN = %q, want %q", item.CommentURN, commentURN)
	}
	if item.CommentActorURN != testMemberURN || item.ActorURN != testMemberURN {
		t.Errorf("comment actor URN = %q/%q", item.CommentActorURN, item.ActorURN)
	}
	if item.CommentActorName != testCommentActorName || item.ActorName != testCommentActorName {
		t.Errorf("comment actor name = %q/%q", item.CommentActorName, item.ActorName)
	}
	if item.CommentText != testCommentText || item.Text != testCommentText {
		t.Errorf("comment text = %q/%q", item.CommentText, item.Text)
	}
	if item.CommentedOnURN != testCommentedOnURN {
		t.Errorf("CommentedOnURN = %q, want urn:li:activity:998", item.CommentedOnURN)
	}
	if item.URL != testCommentedOnURL {
		t.Errorf("URL = %q, want commented-on activity URL", item.URL)
	}
}

func TestParseRecentActivityKeepsDistinctTopLevelComments(t *testing.T) {
	firstCommentURN := testCommentURN
	secondCommentURN := "urn:li:comment:(urn:li:activity:998,456)"
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.CommentUpdate",
				"entityUrn": "` + firstCommentURN + `",
				"createdAt": 3000,
				"actor": {"urn": "` + testMemberURN + `"},
				"message": {"text": "First comment"}
			}, {
				"$type": "com.linkedin.voyager.feed.CommentUpdate",
				"entityUrn": "` + secondCommentURN + `",
				"createdAt": 2000,
				"actor": {"urn": "` + testMemberURN + `"},
				"message": {"text": "Second comment"}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].URN != firstCommentURN {
		t.Errorf("first URN = %q, want %q", items[0].URN, firstCommentURN)
	}
	if items[1].URN != secondCommentURN {
		t.Errorf("second URN = %q, want %q", items[1].URN, secondCommentURN)
	}
	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryComments)
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
}

func TestParseRecentActivityDoesNotFabricateCommentDetailsFromPlainActorMessage(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"actor": {"urn": "` + testMemberURN + `", "name": {"text": "Jane Doe"}},
				"message": {"text": "Top-level message is not a comment"},
				"commentary": {"text": {"text": "Plain update text"}}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.ContentCategory == RecentActivityCategoryComments {
		t.Errorf("ContentCategory = %q, want non-comment", item.ContentCategory)
	}
	if item.CommentURN != "" || item.CommentActorURN != "" || item.CommentActorName != "" || item.CommentText != "" {
		t.Errorf("fabricated comment fields: urn=%q actor=%q name=%q text=%q", item.CommentURN, item.CommentActorURN, item.CommentActorName, item.CommentText)
	}
	if item.ActorURN != testMemberURN {
		t.Errorf("ActorURN = %q, want original actor", item.ActorURN)
	}
	if item.Text != "Plain update text" {
		t.Errorf("Text = %q, want plain update text", item.Text)
	}
}

func TestParseRecentActivityCommentsOnlyExplicitSignals(t *testing.T) {
	resp := &VoyagerResponse{
		Data: []byte(`{
			"elements": [{
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:1",
				"commentUrn": "` + testCommentURN + `"
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:2",
				"socialActivityCounts": {"numComments": 5}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:3",
				"tracking": {"label": "commented by someone else"}
			}, {
				"$type": "com.linkedin.voyager.feed.Update",
				"entityUrn": "urn:li:activity:4",
				"commentary": {"text": {"text": "ordinary post commentary"}}
			}]
		}`),
	}

	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		t.Fatalf("parseRecentActivityFromResponse error: %v", err)
	}
	filtered := filterRecentActivityByCategory(items, RecentActivityCategoryComments)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].URN != testCommentURN {
		t.Errorf("URN = %q, want explicit comment URN", filtered[0].URN)
	}
}

func TestActivityItemJSONOmitsZeroCreatedAt(t *testing.T) {
	data, err := json.Marshal(ActivityItem{
		URN:    testActivityURN,
		Type:   "com.linkedin.voyager.feed.Update",
		RawURN: "urn:li:fs_feedUpdate:(V2&MEMBER_SHARES,urn:li:activity:7475116029644414976)",
	})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(data) != `{"urn":"urn:li:activity:7475116029644414976","type":"com.linkedin.voyager.feed.Update","rawUrn":"urn:li:fs_feedUpdate:(V2\u0026MEMBER_SHARES,urn:li:activity:7475116029644414976)"}` {
		t.Errorf("JSON = %s, want no zero createdAt", data)
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
		Data: []byte(`{
			"elements": [{"$type":"com.linkedin.voyager.feed.Update","createdAt":1000}]
		}`),
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

type graphQLPostsRequest struct {
	Username        string
	ProfileURN      string
	Category        RecentActivityCategory
	QueryID         string
	Count           string
	Start           string
	PaginationToken string
}

func assertGraphQLPostsRequest(t *testing.T, r *http.Request, want *graphQLPostsRequest) {
	t.Helper()

	category := want.Category
	if category == "" {
		category = RecentActivityCategoryPosts
	}
	queryID := want.QueryID
	if queryID == "" {
		queryID = testProfilePostsQueryID
	}
	if r.Header.Get("Referer") != "https://www.linkedin.com/in/"+want.Username+"/recent-activity/"+string(category)+"/" {
		t.Errorf("Referer = %q, want %s activity URL", r.Header.Get("Referer"), category)
	}
	query := r.URL.Query()
	if query.Get("includeWebMetadata") != "true" {
		t.Errorf("includeWebMetadata = %q, want true", query.Get("includeWebMetadata"))
	}
	if !strings.HasPrefix(query.Get("queryId"), "voyagerFeedDashProfileUpdates.") {
		t.Errorf("queryId = %q, want voyagerFeedDashProfileUpdates prefix", query.Get("queryId"))
	}
	if query.Get("queryId") != queryID {
		t.Errorf("queryId = %q, want %q", query.Get("queryId"), queryID)
	}

	variables := query.Get("variables")
	for _, part := range []string{
		"count:" + want.Count,
		"start:" + want.Start,
		"profileUrn:" + want.ProfileURN,
	} {
		if !strings.Contains(variables, part) {
			t.Errorf("variables = %q, missing %q", variables, part)
		}
	}
	if want.PaginationToken != "" && !strings.Contains(variables, "paginationToken:"+want.PaginationToken) {
		t.Errorf("variables = %q, missing paginationToken", variables)
	}
	if want.PaginationToken == "" && strings.Contains(variables, "paginationToken:") {
		t.Errorf("variables = %q, want no paginationToken", variables)
	}
}

func assertGraphQLRawQuery(t *testing.T, r *http.Request, want *graphQLPostsRequest) {
	t.Helper()

	wantQuery := "includeWebMetadata=true&variables=(count:" + want.Count + ",start:" + want.Start + ",profileUrn:" + url.QueryEscape(want.ProfileURN)
	if want.PaginationToken != "" {
		wantQuery += ",paginationToken:" + url.QueryEscape(want.PaginationToken)
	}
	wantQuery += ")&queryId=" + want.QueryID

	if r.URL.RawQuery != wantQuery {
		t.Errorf("RawQuery = %q, want %q", r.URL.RawQuery, wantQuery)
	}
}

func collectionForTestCategory(category RecentActivityCategory) string {
	switch category {
	case RecentActivityCategoryPosts:
		return "feedDashProfileUpdatesByMemberShareFeed"
	case RecentActivityCategoryComments:
		return "feedDashProfileUpdatesByMemberComments"
	case RecentActivityCategoryReactions:
		return "feedDashProfileUpdatesByMemberReactions"
	case RecentActivityCategoryAll,
		RecentActivityCategoryImages,
		RecentActivityCategoryVideos,
		RecentActivityCategoryDocuments,
		RecentActivityCategoryEvents:
		panic(fmt.Sprintf("unsupported test category %q", category))
	}

	panic(fmt.Sprintf("unknown test category %q", category))
}

func writeProfileResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	const profileURN = "urn:li:fsd_profile:abc123"
	writeProfileURNResponse(t, w, profileURN)
}

func writeProfileURNResponse(t *testing.T, w http.ResponseWriter, profileURN string) {
	t.Helper()
	writeJSON(t, w, `{
		"data": {"*elements": ["`+profileURN+`"]},
		"included": [{"entityUrn": "`+profileURN+`", "publicIdentifier": "johndoe", "firstName": "John"}]
	}`)
}

func writeGraphQLProfileUpdatePage(t *testing.T, w http.ResponseWriter, collectionName, activityURN, paginationToken string) {
	t.Helper()
	writeJSON(t, w, `{
		"data": {"data": {"`+collectionName+`": {
			"metadata": {"paginationToken": "`+paginationToken+`"},
			"elements": [{
				"$type": "com.linkedin.voyager.dash.feed.Update",
				"metadata": {"backendUrn": "`+activityURN+`"},
				"commentary": {"text": {"text": "activity"}}
			}]
		}}}
	}`)
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write response: %v", err)
	}
}
