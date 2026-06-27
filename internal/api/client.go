package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
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

var supportedProxySchemes = []string{"http", "https", "socks5"}

// Client is a LinkedIn Voyager API client.
type Client struct {
	httpClient                *http.Client
	baseURL                   string
	credentials               *Credentials
	proxyURL                  string
	configErr                 error
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

// WithProxyURL sets an explicit proxy URL for LinkedIn API requests.
func WithProxyURL(proxyURL string) ClientOption {
	return func(c *Client) {
		c.proxyURL = strings.TrimSpace(proxyURL)
		if c.proxyURL != "" {
			_, err := parseProxyURL(c.proxyURL)
			c.configErr = err
		}
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
			Transport: http.DefaultTransport,
			Timeout:   DefaultTimeout,
			// Don't follow redirects - LinkedIn API redirects indicate auth issues.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		baseURL:                   BaseURL,
		authenticatedRequestDelay: DefaultAuthenticatedRequestDelay,
		recentActivityGraphQL:     defaultRecentActivityGraphQLConfig(),
	}

	for _, opt := range opts {
		opt(c)
	}
	if c.proxyURL != "" && c.configErr == nil {
		c.httpClient = c.httpClientWithProxy(c.proxyURL)
	}

	return c
}

func (c *Client) httpClientWithProxy(proxyURL string) *http.Client {
	transport, err := TransportWithProxy(c.httpClient.Transport, proxyURL)
	if err != nil {
		c.configErr = err
		return c.httpClient
	}

	return &http.Client{
		Transport: transport,
		Timeout:   c.httpClient.Timeout,
		Jar:       c.httpClient.Jar,
		// Don't follow redirects - LinkedIn API redirects indicate auth issues.
		CheckRedirect: c.httpClient.CheckRedirect,
	}
}

// TransportWithProxy returns a transport configured with the validated proxy URL.
func TransportWithProxy(base http.RoundTripper, proxyURL string) (http.RoundTripper, error) {
	proxy, err := parseProxyURL(strings.TrimSpace(proxyURL))
	if err != nil {
		return nil, err
	}

	transport, ok := base.(*http.Transport)
	if base != nil && !ok {
		return nil, fmt.Errorf("proxy URL requires *http.Transport, got %T", base)
	}
	if transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	clone := transport.Clone()
	clone.Proxy = http.ProxyURL(proxy)
	return clone, nil
}

func parseProxyURL(proxyURL string) (*url.URL, error) {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL %q: %w", Redact(proxyURL), err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("invalid proxy URL %q: scheme and host are required", redactedProxyURLForError(proxyURL))
	}
	if !slices.Contains(supportedProxySchemes, parsed.Scheme) {
		return nil, fmt.Errorf("invalid proxy URL %q: unsupported scheme %q", redactedProxyURLForError(proxyURL), parsed.Scheme)
	}
	return parsed, nil
}

func redactedProxyURLForError(proxyURL string) string {
	parsed, err := url.Parse(proxyURL)
	if err != nil {
		return Redact(proxyURL)
	}
	if parsed.User != nil {
		parsed.User = url.UserPassword("[REDACTED]", "[REDACTED]")
	}
	return parsed.String()
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
	RawQuery    string
	Body        any
	Headers     map[string]string
	RequireAuth bool
}

// Do executes an API request and decodes the response.
func (c *Client) Do(ctx context.Context, req *Request, result any) error {
	if err := c.checkConfig(); err != nil {
		return err
	}

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
			Message: fmt.Sprintf("network error: %v", c.sanitizeErrorMessage(err.Error())),
		}
	}
	defer resp.Body.Close()

	return c.handleResponse(resp, result)
}

func (c *Client) checkConfig() error {
	if c.configErr != nil {
		return &Error{Code: ErrCodeInvalidInput, Message: c.configErr.Error()}
	}
	return nil
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
	if req.RawQuery != "" {
		u.RawQuery = req.RawQuery
	} else if req.Query != nil {
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
		return classifyRedirect(c.sanitizeErrorMessage(resp.Header.Get("Location")))
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
			msg = fmt.Sprintf("%s: %s", msg, c.sanitizeErrorMessage(string(body)))
		}
		return &Error{
			Code:    ErrCodeServerError,
			Message: msg,
		}
	}
}

func (c *Client) sanitizeErrorMessage(message string) string {
	redacted := message
	if c.credentials != nil {
		redacted = redactSecrets(redacted, c.credentials.LiAt, c.credentials.JSessID, c.credentials.CSRFToken)
	}
	if c.proxyURL != "" {
		redacted = redactSecrets(redacted, c.proxyURL, Redact(c.proxyURL))
	}
	return Redact(redacted)
}

func redactSecrets(message string, secrets ...string) string {
	redacted := message
	for _, secret := range secrets {
		if secret != "" {
			redacted = strings.ReplaceAll(redacted, secret, "[REDACTED]")
		}
	}
	return redacted
}

// Redact removes LinkedIn and proxy secrets from text safe for errors and logs.
func Redact(text string) string {
	redacted := redactProxyUserinfo(text)
	redacted = redactCookieHeader(redacted)
	return redactSensitiveAssignments(redacted)
}

func redactProxyUserinfo(text string) string {
	searchStart := 0
	for {
		index := strings.Index(text[searchStart:], "://")
		if index < 0 {
			return text
		}
		schemeStart := searchStart + index
		urlStart := schemeStart
		for urlStart > 0 && isURLSchemeChar(rune(text[urlStart-1])) {
			urlStart--
		}
		hostStart := schemeStart + len("://")
		urlEnd := len(text)
		for offset, char := range text[hostStart:] {
			if isURLTerminator(char) {
				urlEnd = hostStart + offset
				break
			}
		}
		candidate := text[urlStart:urlEnd]
		parsed, err := url.Parse(candidate)
		if err != nil || parsed.User == nil || parsed.Host == "" {
			searchStart = hostStart
			continue
		}
		parsed.User = url.UserPassword("[REDACTED]", "[REDACTED]")
		text = text[:urlStart] + parsed.String() + text[urlEnd:]
		searchStart = urlStart + len(parsed.String())
	}
}

func isURLSchemeChar(char rune) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '+' || char == '-' || char == '.'
}

func isURLTerminator(char rune) bool {
	return strings.ContainsRune(" \t\n\r\"')]}>", char)
}

func redactCookieHeader(text string) string {
	for _, marker := range []string{"Cookie:", "Cookie=", "Cookie "} {
		searchStart := 0
		for {
			index := strings.Index(text[searchStart:], marker)
			if index < 0 {
				break
			}
			valueStart := searchStart + index + len(marker)
			valueEnd := len(text)
			if newline := strings.IndexByte(text[valueStart:], '\n'); newline >= 0 {
				valueEnd = valueStart + newline
			}
			text = text[:valueStart] + " [REDACTED]" + text[valueEnd:]
			searchStart = valueStart + len(" [REDACTED]")
		}
	}
	return text
}

func redactSensitiveAssignments(text string) string {
	for _, key := range []string{"li_at", "JSESSIONID", "jsessionid", "csrf-token", "csrf_token", "Csrf-Token"} {
		for _, separator := range []string{"=", ":"} {
			text = redactAssignment(text, key, separator)
		}
	}
	return text
}

func redactAssignment(text, key, separator string) string {
	pattern := key + separator
	searchStart := 0
	for {
		index := strings.Index(text[searchStart:], pattern)
		if index < 0 {
			return text
		}
		valueStart := searchStart + index + len(pattern)
		for valueStart < len(text) && text[valueStart] == ' ' {
			valueStart++
		}
		valueEnd := valueStart
		for valueEnd < len(text) && !strings.ContainsRune(";,& \t\n\r\"'<>)}]", rune(text[valueEnd])) {
			valueEnd++
		}
		text = text[:valueStart] + "[REDACTED]" + text[valueEnd:]
		searchStart = valueStart + len("[REDACTED]")
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
