package linkedin_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bvgroup-co/lnk/linkedin"
)

const (
	testLiAt       = "test-li-at-secret"
	testJSessionID = "test-jsession-secret"
	testCSRFToken  = "test-csrf-secret"
)

func TestPackageIsImportable(t *testing.T) {
	client := linkedin.NewClient()
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
}

func TestClientTestAuthUsesCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/identity/dash/profiles" {
			t.Fatalf("path = %q, want /identity/dash/profiles", r.URL.Path)
		}
		assertAuthenticatedRequest(t, r)
		writeProfileResponse(t, w, "urn:li:fsd_profile:me", "me")
	}))
	defer server.Close()

	client := linkedin.NewClient(
		linkedin.WithBaseURL(server.URL),
		linkedin.WithCredentials(testCredentials()),
		linkedin.WithAuthenticatedRequestDelay(0),
	)

	profile, err := client.TestAuth(context.Background())
	if err != nil {
		t.Fatalf("TestAuth error: %v", err)
	}
	if profile.URN != "urn:li:fsd_profile:me" || profile.PublicID != "me" {
		t.Fatalf("profile = %#v", profile)
	}
}

func TestClientGetProfilePostsUsesActualAPI(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)
		assertAuthenticatedRequest(t, r)

		switch r.URL.Path {
		case "/voyagerIdentityDashProfiles":
			if got := r.URL.Query().Get("memberIdentity"); got != "johndoe" {
				t.Fatalf("memberIdentity = %q, want johndoe", got)
			}
			writeProfileResponse(t, w, "urn:li:fsd_profile:johndoe", "johndoe")
		case "/graphql":
			if !strings.Contains(r.URL.RawQuery, "voyagerFeedDashProfileUpdates") {
				t.Fatalf("GraphQL query = %q, want profile updates query", r.URL.RawQuery)
			}
			if !strings.Contains(r.URL.RawQuery, "profileUrn:urn%3Ali%3Afsd_profile%3Ajohndoe") {
				t.Fatalf("GraphQL query = %q, want encoded profile URN", r.URL.RawQuery)
			}
			writeGraphQLPostsResponse(t, w)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	client := linkedin.NewClient(
		linkedin.WithBaseURL(server.URL),
		linkedin.WithCredentials(testCredentials()),
		linkedin.WithAuthenticatedRequestDelay(0),
	)

	items, err := client.GetProfilePosts(context.Background(), "johndoe", 5)
	if err != nil {
		t.Fatalf("GetProfilePosts error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	item := items[0]
	if item.URN != "urn:li:activity:1" || item.Text != "hello from linkedin" {
		t.Fatalf("item = %#v", item)
	}
	if item.ContentCategory != linkedin.RecentActivityCategoryPosts {
		t.Fatalf("category = %q, want posts", item.ContentCategory)
	}
	wantActor := &linkedin.ActivityActor{
		URN:              "urn:li:member:123",
		PublicIdentifier: "jane-doe",
		ProfileURL:       "https://www.linkedin.com/in/jane-doe",
		FirstName:        "Jane",
		LastName:         "Doe",
		DisplayName:      "Jane Doe",
	}
	if item.Actor == nil || *item.Actor != *wantActor {
		t.Fatalf("actor = %#v, want %#v", item.Actor, wantActor)
	}
	if strings.Join(requests, ",") != "/voyagerIdentityDashProfiles,/graphql" {
		t.Fatalf("requests = %v", requests)
	}
}

func TestClientErrorsDoNotExposeCredentialValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthenticatedRequest(t, r)
		http.Error(w, "upstream echoed "+testLiAt+" "+testJSessionID+" "+testCSRFToken, http.StatusInternalServerError)
	}))
	defer server.Close()

	client := linkedin.NewClient(
		linkedin.WithBaseURL(server.URL),
		linkedin.WithCredentials(testCredentials()),
		linkedin.WithAuthenticatedRequestDelay(0),
	)

	_, err := client.TestAuth(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *linkedin.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *linkedin.Error, got %T", err)
	}
	if apiErr.Code != linkedin.ErrCodeServerError {
		t.Fatalf("code = %q, want %q", apiErr.Code, linkedin.ErrCodeServerError)
	}
	for _, secret := range []string{testLiAt, testJSessionID, testCSRFToken} {
		if strings.Contains(apiErr.Message, secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed credential value %q: %v", secret, err)
		}
	}
}

func TestAsError(t *testing.T) {
	_, err := linkedin.ParseRecentActivityCategory("articles")
	if err == nil {
		t.Fatal("expected error")
	}

	apiErr, ok := linkedin.AsError(err)
	if !ok {
		t.Fatalf("AsError did not recognize %T", err)
	}
	if apiErr.Code != linkedin.ErrCodeInvalidInput {
		t.Fatalf("code = %q, want %q", apiErr.Code, linkedin.ErrCodeInvalidInput)
	}
}

func assertAuthenticatedRequest(t *testing.T, r *http.Request) {
	t.Helper()

	cookie := r.Header.Get("Cookie")
	for _, want := range []string{"li_at=" + testLiAt, "JSESSIONID=" + testJSessionID} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("Cookie = %q, want %q", cookie, want)
		}
	}
	if got := r.Header.Get("Csrf-Token"); got != testCSRFToken {
		t.Fatalf("Csrf-Token = %q, want %q", got, testCSRFToken)
	}
}

func testCredentials() *linkedin.Credentials {
	return &linkedin.Credentials{
		LiAt:      testLiAt,
		JSessID:   testJSessionID,
		CSRFToken: testCSRFToken,
	}
}

func writeProfileResponse(t *testing.T, w http.ResponseWriter, urn, publicID string) {
	t.Helper()
	writeJSON(t, w, `{
		"data": {"*elements": ["`+urn+`"]},
		"included": [{"entityUrn": "`+urn+`", "publicIdentifier": "`+publicID+`", "firstName": "John", "lastName": "Doe"}]
	}`)
}

func writeGraphQLPostsResponse(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeJSON(t, w, `{
		"data": {"feedDashProfileUpdatesByMemberShareFeed": {"metadata": {"paginationToken": ""}, "elements": [{
			"$type": "com.linkedin.voyager.dash.feed.Update",
			"metadata": {"backendUrn": "urn:li:activity:1"},
			"actor": {
				"urn": "urn:li:member:123",
				"publicIdentifier": "jane-doe",
				"profileUrl": "https://www.linkedin.com/in/jane-doe",
				"firstName": "Jane",
				"lastName": "Doe",
				"name": {"text": "Jane Doe"}
			},
			"commentary": {"text": {"text": "hello from linkedin"}}
		}]}}
	}`)
}

func TestActivityActorTypeIsPubliclyNameable(t *testing.T) {
	actor := linkedin.ActivityActor{URN: "urn:li:member:123"}
	item := linkedin.ActivityItem{Actor: &actor}
	if item.Actor == nil || item.Actor.URN != actor.URN {
		t.Fatalf("actor = %#v, want public ActivityActor", item.Actor)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func TestClientWithProxyURLUsesProxy(t *testing.T) {
	proxyRequests := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- r.URL.String()
		assertAuthenticatedRequest(t, r)
		writeProfileResponse(t, w, "urn:li:fsd_profile:me", "me")
	}))
	defer proxy.Close()

	client := linkedin.NewClient(
		linkedin.WithBaseURL("http://linkedin.test/voyager/api"),
		linkedin.WithCredentials(testCredentials()),
		linkedin.WithAuthenticatedRequestDelay(0),
		linkedin.WithProxyURL(proxy.URL),
	)

	profile, err := client.TestAuth(context.Background())
	if err != nil {
		t.Fatalf("TestAuth error: %v", err)
	}
	if profile.PublicID != "me" {
		t.Fatalf("profile = %#v", profile)
	}

	select {
	case got := <-proxyRequests:
		if got != "http://linkedin.test/voyager/api/identity/dash/profiles?q=memberIdentity&memberIdentity=me&decorationId=com.linkedin.voyager.dash.deco.identity.profile.WebTopCardCore-19" {
			t.Fatalf("proxied URL = %q, want LinkedIn profile URL", got)
		}
	default:
		t.Fatal("proxy did not receive request")
	}
}

func TestClientInvalidProxyURLIsSanitized(t *testing.T) {
	client := linkedin.NewClient(
		linkedin.WithBaseURL("http://linkedin.test/voyager/api"),
		linkedin.WithCredentials(testCredentials()),
		linkedin.WithAuthenticatedRequestDelay(0),
		linkedin.WithProxyURL("http://user:proxy-password@"),
	)

	_, err := client.TestAuth(context.Background())
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
	var apiErr *linkedin.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *linkedin.Error, got %T", err)
	}
	if apiErr.Code != linkedin.ErrCodeInvalidInput {
		t.Fatalf("code = %q, want %q", apiErr.Code, linkedin.ErrCodeInvalidInput)
	}
	for _, secret := range []string{"user", "proxy-password"} {
		if strings.Contains(apiErr.Message, secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}
