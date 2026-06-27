package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient()
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.baseURL != BaseURL {
		t.Errorf("expected baseURL %q, got %q", BaseURL, c.baseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestClientWithOptions(t *testing.T) {
	customHTTP := &http.Client{Timeout: 60 * time.Second}
	customURL := "https://custom.example.com"
	creds := &Credentials{LiAt: "test", JSessID: "session"}
	requestDelay := 25 * time.Millisecond
	graphQLConfig := RecentActivityGraphQLConfig{ProfilePostsQueryID: "voyagerFeedDashProfileUpdates.test"}

	c := NewClient(
		WithHTTPClient(customHTTP),
		WithBaseURL(customURL),
		WithCredentials(creds),
		WithAuthenticatedRequestDelay(requestDelay),
		WithRecentActivityGraphQLConfig(graphQLConfig),
	)

	if c.httpClient != customHTTP {
		t.Error("custom HTTP client not set")
	}
	if c.baseURL != customURL {
		t.Errorf("expected baseURL %q, got %q", customURL, c.baseURL)
	}
	if c.credentials != creds {
		t.Error("credentials not set")
	}
	if c.authenticatedRequestDelay != requestDelay {
		t.Errorf("expected request delay %s, got %s", requestDelay, c.authenticatedRequestDelay)
	}
	if c.recentActivityGraphQL != graphQLConfig {
		t.Errorf("expected GraphQL config %#v, got %#v", graphQLConfig, c.recentActivityGraphQL)
	}
}

func TestCredentialsIsValid(t *testing.T) {
	tests := []struct {
		name  string
		creds Credentials
		want  bool
	}{
		{
			name:  "empty credentials",
			creds: Credentials{},
			want:  false,
		},
		{
			name:  "missing JSessID",
			creds: Credentials{LiAt: "token"},
			want:  false,
		},
		{
			name:  "missing LiAt",
			creds: Credentials{JSessID: "session"},
			want:  false,
		},
		{
			name:  "valid credentials",
			creds: Credentials{LiAt: "token", JSessID: "session"},
			want:  true,
		},
		{
			name:  "expired credentials",
			creds: Credentials{LiAt: "token", JSessID: "session", ExpiresAt: time.Now().Add(-1 * time.Hour)},
			want:  false,
		},
		{
			name:  "not expired credentials",
			creds: Credentials{LiAt: "token", JSessID: "session", ExpiresAt: time.Now().Add(1 * time.Hour)},
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.creds.IsValid()
			if got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClientHasCredentials(t *testing.T) {
	c := newTestClient()
	if c.HasCredentials() {
		t.Error("expected HasCredentials() to be false without credentials")
	}

	c.SetCredentials(&Credentials{LiAt: "token", JSessID: "session"})
	if !c.HasCredentials() {
		t.Error("expected HasCredentials() to be true with valid credentials")
	}
}

func TestClientDoWithMockServer(t *testing.T) {
	// Create a mock server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("User-Agent") != UserAgent {
			t.Errorf("unexpected User-Agent: %q", r.Header.Get("User-Agent"))
		}
		if r.Header.Get("X-Restli-Protocol-Version") != "2.0.0" {
			t.Errorf("unexpected X-Restli-Protocol-Version: %q", r.Header.Get("X-Restli-Protocol-Version"))
		}
		assertAuthenticatedVoyagerHeaders(t, r)

		// Verify auth headers when credentials are set.
		cookie := r.Header.Get("Cookie")
		if cookie != "" {
			if r.Header.Get("Csrf-Token") == "" {
				t.Error("Csrf-Token header missing when authenticated")
			}
		}

		// Return mock response.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	// Test without auth.
	c := newTestClient(WithBaseURL(server.URL))
	c.SetCredentials(&Credentials{LiAt: "test-token", JSessID: "test-session"})

	var result map[string]string
	err := c.Get(context.Background(), "/test", nil, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestClientPacesAuthenticatedRequests(t *testing.T) {
	requestDelay := 20 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithAuthenticatedRequestDelay(requestDelay),
	)

	start := time.Now()
	err := c.Get(context.Background(), "/test", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < requestDelay {
		t.Errorf("authenticated request elapsed %s, want at least %s", elapsed, requestDelay)
	}
}

func TestClientCanDisableAuthenticatedRequestPacing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithAuthenticatedRequestDelay(0),
	)

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	cancel()

	err := c.Get(ctx, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestClientAuthenticatedRequestPacingHonorsContext(t *testing.T) {
	c := NewClient(
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithAuthenticatedRequestDelay(time.Hour),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Get(ctx, "/test", nil, nil)
	if err == nil {
		t.Fatal("expected pacing error")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeNetworkError {
		t.Errorf("expected error code %q, got %q", ErrCodeNetworkError, apiErr.Code)
	}
}

func TestClientDoRequiresAuth(t *testing.T) {
	c := newTestClient()

	err := c.Do(context.Background(), &Request{
		Method:      http.MethodGet,
		Path:        "/test",
		RequireAuth: true,
	}, nil)

	if err == nil {
		t.Fatal("expected error for unauthenticated request")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeAuthRequired {
		t.Errorf("expected error code %q, got %q", ErrCodeAuthRequired, apiErr.Code)
	}
}

func TestClientHandleErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantCode   string
	}{
		{"unauthorized", http.StatusUnauthorized, ErrCodeAuthExpired},
		{"forbidden", http.StatusForbidden, ErrCodeForbidden},
		{"not found", http.StatusNotFound, ErrCodeNotFound},
		{"rate limited", http.StatusTooManyRequests, ErrCodeRateLimited},
		{"server error", http.StatusInternalServerError, ErrCodeServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			c := newTestClient(
				WithBaseURL(server.URL),
				WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
			)

			err := c.Get(context.Background(), "/test", nil, nil)
			if err == nil {
				t.Fatal("expected error")
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *Error, got %T", err)
			}
			if apiErr.Code != tt.wantCode {
				t.Errorf("expected error code %q, got %q", tt.wantCode, apiErr.Code)
			}
		})
	}
}

func TestClientClassifiesHardStopRedirects(t *testing.T) {
	tests := []struct {
		name        string
		location    string
		messagePart string
	}{
		{
			name:        "login redirect",
			location:    "https://www.linkedin.com/login?session_redirect=%2Fvoyager%2Fapi%2Ffeed%2Fupdates",
			messagePart: "login required",
		},
		{
			name:        "checkpoint redirect",
			location:    "https://www.linkedin.com/checkpoint/challenge/abc",
			messagePart: "checkpoint detected",
		},
		{
			name:        "challenge redirect",
			location:    "https://www.linkedin.com/challenge/abc",
			messagePart: "challenge detected",
		},
		{
			name:        "authwall redirect",
			location:    "https://www.linkedin.com/authwall?trk=bf",
			messagePart: "authwall detected",
		},
		{
			name:        "security verification redirect",
			location:    "https://www.linkedin.com/checkpoint/lg/security-verification",
			messagePart: "security verification detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", tt.location)
				w.WriteHeader(http.StatusFound)
			}))
			defer server.Close()

			c := newTestClient(
				WithBaseURL(server.URL),
				WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
			)

			err := c.Get(context.Background(), "/test", nil, nil)
			if err == nil {
				t.Fatal("expected error")
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("expected *Error, got %T", err)
			}
			if apiErr.Code != ErrCodeAuthExpired {
				t.Errorf("expected error code %q, got %q", ErrCodeAuthExpired, apiErr.Code)
			}
			if !strings.Contains(apiErr.Message, tt.messagePart) {
				t.Errorf("expected message containing %q, got %q", tt.messagePart, apiErr.Message)
			}
		})
	}
}

func TestClientPost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode body: %v", err)
		}
		if body["text"] != "test post" {
			t.Errorf("unexpected body: %v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "123"})
	}))
	defer server.Close()

	c := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
	)

	var result map[string]string
	err := c.Post(context.Background(), "/posts", map[string]string{"text": "test post"}, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["id"] != "123" {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestClientQueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "10" {
			t.Errorf("expected limit=10, got %s", r.URL.Query().Get("limit"))
		}
		if r.URL.Query().Get("start") != "0" {
			t.Errorf("expected start=0, got %s", r.URL.Query().Get("start"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
	)

	query := url.Values{}
	query.Set("limit", "10")
	query.Set("start", "0")

	err := c.Get(context.Background(), "/feed", query, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertAuthenticatedVoyagerHeaders(t *testing.T, r *http.Request) {
	t.Helper()

	wantHeaders := map[string]string{
		"Referer":        "https://www.linkedin.com/feed/",
		"Origin":         "https://www.linkedin.com",
		"Sec-Fetch-Dest": "empty",
		"Sec-Fetch-Mode": "cors",
		"Sec-Fetch-Site": "same-origin",
	}
	for header, want := range wantHeaders {
		if got := r.Header.Get(header); got != want {
			t.Errorf("expected %s %q, got %q", header, want, got)
		}
	}
}

func newTestClient(opts ...ClientOption) *Client {
	return NewClient(append(opts, WithAuthenticatedRequestDelay(0))...)
}

func TestErrorInterface(t *testing.T) {
	err := &Error{Code: ErrCodeAuthExpired, Message: "session expired"}
	expected := "[AUTH_EXPIRED] session expired"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestClientUsesExplicitProxyURL(t *testing.T) {
	proxyRequests := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- r.URL.String()
		assertAuthenticatedVoyagerHeaders(t, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "proxied"})
	}))
	defer proxy.Close()

	c := newTestClient(
		WithBaseURL("http://linkedin.test/voyager/api"),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL(proxy.URL),
	)

	var result map[string]string
	if err := c.Get(context.Background(), "/test", nil, &result); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if result["status"] != "proxied" {
		t.Fatalf("result = %v, want proxied", result)
	}

	select {
	case got := <-proxyRequests:
		if got != "http://linkedin.test/voyager/api/test" {
			t.Fatalf("proxied URL = %q, want absolute LinkedIn URL", got)
		}
	default:
		t.Fatal("proxy did not receive request")
	}
}

func TestClientInvalidProxyURLFailsBeforeRequest(t *testing.T) {
	serverHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL("http://user:secret@"),
	)

	err := c.Get(context.Background(), "/test", nil, nil)
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
	if serverHit {
		t.Fatal("request reached server despite invalid proxy")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Fatalf("code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
	for _, secret := range []string{"user", "secret"} {
		if strings.Contains(apiErr.Message, secret) || strings.Contains(err.Error(), secret) {
			t.Fatalf("invalid proxy error leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(apiErr.Message, "invalid proxy URL") {
		t.Fatalf("message = %q, want invalid proxy URL", apiErr.Message)
	}
}

func TestClientWithProxyURLPreservesProvidedHTTPTransport(t *testing.T) {
	proxyRequests := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyRequests <- r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "proxied"})
	}))
	defer proxy.Close()

	baseTransport := &http.Transport{ResponseHeaderTimeout: time.Second}
	customClient := &http.Client{
		Transport: baseTransport,
		Timeout:   2 * time.Second,
	}

	c := newTestClient(
		WithHTTPClient(customClient),
		WithBaseURL("http://linkedin.test/voyager/api"),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL(proxy.URL),
	)

	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.httpClient.Transport)
	}
	if transport == baseTransport {
		t.Fatal("proxy transport reused caller transport instead of cloning it")
	}
	if transport.ResponseHeaderTimeout != baseTransport.ResponseHeaderTimeout {
		t.Fatalf("ResponseHeaderTimeout = %s, want %s", transport.ResponseHeaderTimeout, baseTransport.ResponseHeaderTimeout)
	}
	if c.httpClient.Timeout != customClient.Timeout {
		t.Fatalf("Timeout = %s, want %s", c.httpClient.Timeout, customClient.Timeout)
	}

	var result map[string]string
	if err := c.Get(context.Background(), "/test", nil, &result); err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if result["status"] != "proxied" {
		t.Fatalf("result = %v, want proxied", result)
	}

	select {
	case got := <-proxyRequests:
		if got != "http://linkedin.test/voyager/api/test" {
			t.Fatalf("proxied URL = %q, want absolute LinkedIn URL", got)
		}
	default:
		t.Fatal("proxy did not receive request")
	}
}

func TestClientWithProxyURLRejectsCustomRoundTripperBeforeRequest(t *testing.T) {
	c := newTestClient(
		WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("custom round tripper should not be called when proxy cannot be applied")
			return nil, nil
		})}),
		WithBaseURL("http://linkedin.test/voyager/api"),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL("http://proxy.example:8080"),
	)

	err := c.Get(context.Background(), "/test", nil, nil)
	if err == nil {
		t.Fatal("expected custom round tripper proxy error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Fatalf("code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
	if !strings.Contains(apiErr.Message, "cannot apply proxy URL to custom RoundTripper") {
		t.Fatalf("message = %q, want loud custom RoundTripper proxy failure", apiErr.Message)
	}
}

func TestGetRecentActivityDebugShapeEndpointInvalidProxyFailsBeforeRequest(t *testing.T) {
	serverHit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := newTestClient(
		WithBaseURL(server.URL),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL("ftp://proxy.example:21"),
	)

	_, err := c.getRecentActivityDebugShapeEndpoint(context.Background(), recentActivityEndpoint{path: "/test"})
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
	if serverHit {
		t.Fatal("request reached server despite invalid proxy")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeInvalidInput {
		t.Fatalf("code = %q, want %q", apiErr.Code, ErrCodeInvalidInput)
	}
	if !strings.Contains(apiErr.Message, `unsupported scheme "ftp"`) {
		t.Fatalf("message = %q, want unsupported proxy scheme", apiErr.Message)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestMalformedProxyURLParseErrorRedactsUserinfo(t *testing.T) {
	c := newTestClient(
		WithBaseURL("http://linkedin.test/voyager/api"),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL("http://user:sec%zz@proxy.example:8080"),
	)

	err := c.Get(context.Background(), "/test", nil, nil)
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
	for _, secret := range []string{"user", "sec%zz"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("invalid proxy error leaked %q: %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "[REDACTED]@proxy.example:8080") {
		t.Fatalf("error = %q, want redacted proxy authority", err.Error())
	}
}

func TestRedactRemovesProxyAndLinkedInSecrets(t *testing.T) {
	input := "proxy http://user:secret@proxy.example:8080 Cookie: li_at=li-secret; JSESSIONID=session-secret\ncsrf-token: csrf-secret https://other:pass@example.com"
	redacted := Redact(input)

	for _, secret := range []string{"user", "secret", "li-secret", "session-secret", "csrf-secret", "other", "pass"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("Redact leaked %q: %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, "[REDACTED]") {
		t.Fatalf("Redact output = %q, want redaction marker", redacted)
	}
}

func TestClientClassifiesRedirectsThroughExplicitProxy(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://www.linkedin.com/checkpoint/challenge/abc")
		w.WriteHeader(http.StatusFound)
	}))
	defer proxy.Close()

	c := newTestClient(
		WithBaseURL("http://linkedin.test/voyager/api"),
		WithCredentials(&Credentials{LiAt: "test", JSessID: "session"}),
		WithProxyURL(proxy.URL),
	)

	err := c.Get(context.Background(), "/test", nil, nil)
	if err == nil {
		t.Fatal("expected redirect classification error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *Error, got %T", err)
	}
	if apiErr.Code != ErrCodeAuthExpired {
		t.Fatalf("code = %q, want %q", apiErr.Code, ErrCodeAuthExpired)
	}
	if !strings.Contains(apiErr.Message, "checkpoint detected") {
		t.Fatalf("message = %q, want checkpoint classification", apiErr.Message)
	}
}
