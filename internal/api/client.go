package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// BaseURL is the LinkedIn Voyager API base URL.
	BaseURL = "https://www.linkedin.com/voyager/api"

	// DefaultTimeout for HTTP requests.
	DefaultTimeout = 30 * time.Second

	// DefaultAuthenticatedRequestDelay is the conservative pacing delay for authenticated requests.
	DefaultAuthenticatedRequestDelay = 500 * time.Millisecond

	// UserAgent mimics a browser.
	UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// Client is a LinkedIn Voyager API client.
type Client struct {
	httpClient                *http.Client
	baseURL                   string
	credentials               *Credentials
	authenticatedRequestDelay time.Duration
	recentActivityGraphQL     RecentActivityGraphQLConfig
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = hc
	}
}

// WithBaseURL sets a custom base URL (useful for testing).
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithCredentials sets the authentication credentials.
func WithCredentials(creds *Credentials) ClientOption {
	return func(c *Client) {
		c.credentials = creds
	}
}

// WithAuthenticatedRequestDelay sets the pacing delay for authenticated requests.
func WithAuthenticatedRequestDelay(delay time.Duration) ClientOption {
	return func(c *Client) {
		c.authenticatedRequestDelay = delay
	}
}

// WithRecentActivityGraphQLConfig sets GraphQL query configuration for recent activity.
func WithRecentActivityGraphQLConfig(config RecentActivityGraphQLConfig) ClientOption {
	return func(c *Client) {
		c.recentActivityGraphQL = config
	}
}

// NewClient creates a new LinkedIn API client.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		httpClient: &http.Client{
			Timeout: DefaultTimeout,
			// Don't follow redirects - LinkedIn API redirects indicate auth issues.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:                   BaseURL,
		authenticatedRequestDelay: DefaultAuthenticatedRequestDelay,
		recentActivityGraphQL: RecentActivityGraphQLConfig{
			ProfileUpdatesQueryID: defaultProfileUpdatesQueryID,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// SetCredentials updates the client's credentials.
func (c *Client) SetCredentials(creds *Credentials) {
	c.credentials = creds
}

// HasCredentials returns true if credentials are set and valid.
func (c *Client) HasCredentials() bool {
	return c.credentials != nil && c.credentials.IsValid()
}

// Request represents an API request.
type Request struct {
	Method      string
	Path        string
	Query       url.Values
	Body        any
	Headers     map[string]string
	RequireAuth bool
}

// Do executes an API request and decodes the response.
func (c *Client) Do(ctx context.Context, req *Request, result any) error {
	httpReq, err := c.buildRequest(ctx, req)
	if err != nil {
		return err
	}
	pacingErr := c.waitForAuthenticatedRequest(ctx, req)
	if pacingErr != nil {
		return pacingErr
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return &Error{
			Code:    ErrCodeNetworkError,
			Message: fmt.Sprintf("network error: %v", err),
		}
	}
	defer resp.Body.Close()

	return c.handleResponse(resp, result)
}

func (c *Client) waitForAuthenticatedRequest(ctx context.Context, req *Request) error {
	if !req.RequireAuth || c.authenticatedRequestDelay <= 0 {
		return nil
	}

	timer := time.NewTimer(c.authenticatedRequestDelay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return &Error{
			Code:    ErrCodeNetworkError,
			Message: fmt.Sprintf("request canceled during pacing delay: %v", ctx.Err()),
		}
	}
}

// buildRequest creates an HTTP request with proper headers.
func (c *Client) buildRequest(ctx context.Context, req *Request) (*http.Request, error) {
	// Check auth requirement.
	if req.RequireAuth && !c.HasCredentials() {
		return nil, &Error{
			Code:    ErrCodeAuthRequired,
			Message: "authentication required. Run: lnk auth login",
		}
	}

	// Build URL.
	u, err := url.Parse(c.baseURL + req.Path)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if req.Query != nil {
		u.RawQuery = req.Query.Encode()
	}

	// Build body.
	var bodyReader io.Reader
	if req.Body != nil {
		jsonBody, marshalErr := json.Marshal(req.Body)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", marshalErr)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	// Create request.
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set standard headers.
	c.setHeaders(httpReq, req)

	return httpReq, nil
}

// setHeaders adds required headers to the request.
func (c *Client) setHeaders(httpReq *http.Request, req *Request) {
	// Standard headers.
	httpReq.Header.Set("User-Agent", UserAgent)
	httpReq.Header.Set("Accept", "application/vnd.linkedin.normalized+json+2.1")
	httpReq.Header.Set("Accept-Language", "en-US,en;q=0.9")
	httpReq.Header.Set("X-Li-Lang", "en_US")
	httpReq.Header.Set("X-Li-Track", `{"clientVersion":"1.13.8677","mpVersion":"1.13.8677","osName":"web","timezoneOffset":-8,"timezone":"America/Los_Angeles","deviceFormFactor":"DESKTOP","mpName":"voyager-web","displayDensity":2,"displayWidth":3456,"displayHeight":2234}`)
	httpReq.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	// Content type for requests with body.
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	// Authentication headers.
	if c.credentials != nil && c.credentials.IsValid() {
		httpReq.Header.Set("Referer", "https://www.linkedin.com/feed/")
		httpReq.Header.Set("Origin", "https://www.linkedin.com")
		httpReq.Header.Set("Sec-Fetch-Dest", "empty")
		httpReq.Header.Set("Sec-Fetch-Mode", "cors")
		httpReq.Header.Set("Sec-Fetch-Site", "same-origin")

		// Set cookies.
		cookies := []string{
			fmt.Sprintf("li_at=%s", c.credentials.LiAt),
			fmt.Sprintf("JSESSIONID=%s", c.credentials.JSessID),
		}
		httpReq.Header.Set("Cookie", strings.Join(cookies, "; "))

		// Set CSRF token from JSESSIONID.
		csrfToken := c.credentials.CSRFToken
		if csrfToken == "" {
			// Extract from JSESSIONID if not set.
			csrfToken = strings.Trim(c.credentials.JSessID, `"`)
		}
		httpReq.Header.Set("Csrf-Token", csrfToken)
	}

	// Custom headers.
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
}

// handleResponse processes the HTTP response.
func (c *Client) handleResponse(resp *http.Response, result any) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &Error{
			Code:    ErrCodeNetworkError,
			Message: fmt.Sprintf("failed to read response: %v", err),
		}
	}

	// Check for redirects - LinkedIn API redirects indicate auth issues or hard stops.
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		// Check if LinkedIn is clearing our session.
		for _, cookie := range resp.Cookies() {
			if cookie.Name == "li_at" && cookie.Value == "delete me" {
				return &Error{
					Code:    ErrCodeAuthExpired,
					Message: "session invalid or expired. Run: lnk auth login",
				}
			}
		}
		return classifyRedirect(resp.Header.Get("Location"))
	}

	// Check for error status codes.
	if resp.StatusCode >= 400 {
		return c.handleErrorResponse(resp.StatusCode, body)
	}

	// Decode successful response.
	if result != nil && len(body) > 0 {
		if err := json.Unmarshal(body, result); err != nil {
			return &Error{
				Code:    ErrCodeServerError,
				Message: fmt.Sprintf("failed to decode response: %v", err),
			}
		}
	}

	return nil
}

// handleErrorResponse converts HTTP error status to an Error.
func (c *Client) handleErrorResponse(statusCode int, body []byte) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "session expired. Run: lnk auth login",
		}
	case http.StatusForbidden:
		return &Error{
			Code:    ErrCodeForbidden,
			Message: "access denied",
		}
	case http.StatusNotFound:
		return &Error{
			Code:    ErrCodeNotFound,
			Message: "resource not found",
		}
	case http.StatusTooManyRequests:
		return &Error{
			Code:    ErrCodeRateLimited,
			Message: "rate limited. Please wait and try again",
		}
	default:
		msg := fmt.Sprintf("request failed with status %d", statusCode)
		if len(body) > 0 {
			msg = fmt.Sprintf("%s: %s", msg, string(body))
		}
		return &Error{
			Code:    ErrCodeServerError,
			Message: msg,
		}
	}
}

func classifyRedirect(location string) error {
	if location == "" {
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "session redirect detected. Run: lnk auth login",
		}
	}

	redirectURL, err := url.Parse(location)
	if err != nil {
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: fmt.Sprintf("session redirect detected to %s. Run: lnk auth login", location),
		}
	}

	path := strings.ToLower(redirectURL.EscapedPath())
	rawQuery := strings.ToLower(redirectURL.RawQuery)
	redirectTarget := strings.ToLower(location)
	switch {
	case strings.Contains(redirectTarget, "security-verification") || strings.Contains(redirectTarget, "securityverification"):
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "LinkedIn security verification detected. Complete verification in a browser, then run: lnk auth login",
		}
	case strings.Contains(path, "/checkpoint/"):
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "LinkedIn checkpoint detected. Complete verification in a browser, then run: lnk auth login",
		}
	case strings.Contains(path, "/challenge/"):
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "LinkedIn challenge detected. Complete verification in a browser, then run: lnk auth login",
		}
	case strings.Contains(path, "/authwall"):
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "LinkedIn authwall detected. Run: lnk auth login",
		}
	case strings.Contains(path, "/login") || strings.Contains(rawQuery, "session_redirect"):
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: "session expired or login required. Run: lnk auth login",
		}
	default:
		return &Error{
			Code:    ErrCodeAuthExpired,
			Message: fmt.Sprintf("session redirect detected to %s. Run: lnk auth login", location),
		}
	}
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, path string, query url.Values, result any) error {
	return c.Do(ctx, &Request{
		Method:      http.MethodGet,
		Path:        path,
		Query:       query,
		RequireAuth: true,
	}, result)
}

// Post performs a POST request.
func (c *Client) Post(ctx context.Context, path string, body, result any) error {
	return c.Do(ctx, &Request{
		Method:      http.MethodPost,
		Path:        path,
		Body:        body,
		RequireAuth: true,
	}, result)
}

// Delete performs a DELETE request.
func (c *Client) Delete(ctx context.Context, path string) error {
	return c.Do(ctx, &Request{
		Method:      http.MethodDelete,
		Path:        path,
		RequireAuth: true,
	}, nil)
}
