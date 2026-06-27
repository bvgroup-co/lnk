package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bvgroup-co/lnk/internal/api"
)

const (
	linkedInBaseURL = "https://www.linkedin.com"
	loginPageURL    = "https://www.linkedin.com/login"
	loginSubmitURL  = "https://www.linkedin.com/checkpoint/lg/login-submit"
	userAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// LoginWithCredentials authenticates with LinkedIn using email and password.
func LoginWithCredentials(email, password string) (*api.Credentials, error) {
	return LoginWithCredentialsOptions(email, password)
}

// LoginOption configures LinkedIn login requests.
type LoginOption func(*loginConfig)

type loginConfig struct {
	proxyURL string
}

// WithProxyURL sets an explicit proxy URL for LinkedIn login requests.
func WithProxyURL(proxyURL string) LoginOption {
	return func(config *loginConfig) {
		config.proxyURL = strings.TrimSpace(proxyURL)
	}
}

// LoginWithCredentialsOptions authenticates with LinkedIn using email and password.
func LoginWithCredentialsOptions(email, password string, options ...LoginOption) (*api.Credentials, error) {
	config := &loginConfig{}
	for _, option := range options {
		option(config)
	}

	// Create HTTP client with cookie jar.
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
		// Don't follow redirects automatically - we need to check cookies at each step.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if config.proxyURL != "" {
		transport, proxyErr := api.TransportWithProxy(client.Transport, config.proxyURL)
		if proxyErr != nil {
			return nil, proxyErr
		}
		client.Transport = transport
	}

	// Step 1: Get login page to obtain CSRF tokens and initial cookies.
	csrfToken, loginCsrf, err := getLoginTokens(client)
	if err != nil {
		return nil, fmt.Errorf("failed to get login page: %w", err)
	}

	// Step 2: Submit login credentials.
	creds, err := submitLogin(client, email, password, csrfToken, loginCsrf)
	if err != nil {
		return nil, err
	}

	return creds, nil
}

// getLoginTokens fetches the login page and extracts CSRF tokens.
func getLoginTokens(client *http.Client) (csrfToken, loginCsrf string, err error) {
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", loginPageURL, http.NoBody)
	if err != nil {
		return "", "", err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("request failed: %s", api.Redact(err.Error()))
	}

	// Follow redirects manually if needed.
	for resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location := resp.Header.Get("Location")
		if location == "" {
			resp.Body.Close()
			break
		}
		if !strings.HasPrefix(location, "http") {
			location = linkedInBaseURL + location
		}
		resp.Body.Close()
		req, _ = http.NewRequestWithContext(ctx, "GET", location, http.NoBody)
		req.Header.Set("User-Agent", userAgent)
		resp, err = client.Do(req)
		if err != nil {
			return "", "", fmt.Errorf("%s", api.Redact(err.Error()))
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read response: %w", err)
	}

	// Extract CSRF token.
	csrfRegex := regexp.MustCompile(`name="csrfToken"\s*value="([^"]+)"`)
	matches := csrfRegex.FindSubmatch(body)
	if len(matches) < 2 {
		return "", "", fmt.Errorf("csrfToken not found in login page")
	}
	csrfToken = string(matches[1])

	// Extract login CSRF param.
	loginCsrfRegex := regexp.MustCompile(`name="loginCsrfParam"\s*value="([^"]+)"`)
	matches = loginCsrfRegex.FindSubmatch(body)
	if len(matches) < 2 {
		return "", "", fmt.Errorf("loginCsrfParam not found in login page")
	}
	loginCsrf = string(matches[1])

	return csrfToken, loginCsrf, nil
}

// submitLogin submits the login form with credentials.
func submitLogin(client *http.Client, email, password, csrfToken, loginCsrf string) (*api.Credentials, error) {
	ctx := context.Background()

	// Prepare form data.
	formData := url.Values{}
	formData.Set("csrfToken", csrfToken)
	formData.Set("session_key", email)
	formData.Set("session_password", password)
	formData.Set("loginCsrfParam", loginCsrf)

	req, err := http.NewRequestWithContext(ctx, "POST", loginSubmitURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Origin", linkedInBaseURL)
	req.Header.Set("Referer", loginPageURL)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("login request failed: %s", api.Redact(err.Error()))
	}

	// Check for successful login by looking for li_at cookie.
	linkedInURL, _ := url.Parse(linkedInBaseURL)

	// Follow redirect chain to collect all cookies.
	maxRedirects := 10
	for i := 0; i < maxRedirects && (resp.StatusCode >= 300 && resp.StatusCode < 400); i++ {
		location := resp.Header.Get("Location")
		if location == "" {
			resp.Body.Close()
			break
		}

		// Check if redirecting to challenge page (wrong password, 2FA, captcha).
		if strings.Contains(location, "/checkpoint/challenge") {
			resp.Body.Close()
			return nil, fmt.Errorf("login failed: LinkedIn requires verification (wrong password, 2FA, or captcha). Use cookie authentication instead")
		}

		// Check if redirecting to security verification.
		if strings.Contains(location, "security-verification") {
			resp.Body.Close()
			return nil, fmt.Errorf("login failed: security verification required. Use cookie authentication instead")
		}

		// Build absolute URL.
		if !strings.HasPrefix(location, "http") {
			location = linkedInBaseURL + location
		}

		resp.Body.Close()
		req, _ = http.NewRequestWithContext(ctx, "GET", location, http.NoBody)
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("redirect failed: %s", api.Redact(err.Error()))
		}
	}
	defer resp.Body.Close()

	// Extract credentials from cookies.
	creds := &api.Credentials{}

	for _, cookie := range client.Jar.Cookies(linkedInURL) {
		switch cookie.Name {
		case cookieLiAt:
			creds.LiAt = cookie.Value
			if !cookie.Expires.IsZero() {
				creds.ExpiresAt = cookie.Expires
			}
		case cookieJSessionID:
			creds.JSessID = cookie.Value
			creds.CSRFToken = strings.Trim(cookie.Value, `"`)
		}
	}

	// Also check www subdomain.
	wwwURL, _ := url.Parse("https://www.linkedin.com")
	for _, cookie := range client.Jar.Cookies(wwwURL) {
		switch cookie.Name {
		case cookieLiAt:
			if creds.LiAt == "" {
				creds.LiAt = cookie.Value
				if !cookie.Expires.IsZero() {
					creds.ExpiresAt = cookie.Expires
				}
			}
		case cookieJSessionID:
			if creds.JSessID == "" {
				creds.JSessID = cookie.Value
				creds.CSRFToken = strings.Trim(cookie.Value, `"`)
			}
		}
	}

	if creds.LiAt == "" {
		return nil, fmt.Errorf("login failed: invalid email or password")
	}
	if creds.JSessID == "" {
		return nil, fmt.Errorf("login partially succeeded but JSESSIONID not received")
	}

	return creds, nil
}
