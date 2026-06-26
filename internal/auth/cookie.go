package auth

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/bvgroup-co/lnk/internal/api"
)

// Browser represents a supported browser for cookie extraction.
type Browser string

const (
	BrowserSafari   Browser = "safari"
	BrowserChrome   Browser = "chrome"
	BrowserChromium Browser = "chromium"
	BrowserFirefox  Browser = "firefox"
	BrowserBrave    Browser = "brave"
	BrowserEdge     Browser = "edge"
	BrowserArc      Browser = "arc"
	BrowserHelium   Browser = "helium"
	BrowserOpera    Browser = "opera"
	BrowserVivaldi  Browser = "vivaldi"

	// Cookie name constants.
	cookieLiAt       = "li_at"
	cookieJSessionID = "JSESSIONID"

	// OS constants.
	osDarwin = "darwin"
	osLinux  = "linux"
)

// Cookie represents a browser cookie.
type Cookie struct {
	Domain     string
	Name       string
	Value      string
	Path       string
	ExpiresAt  time.Time
	IsSecure   bool
	IsHTTPOnly bool
}

// SupportedBrowsers returns browsers supported on the current platform.
func SupportedBrowsers() []Browser {
	if runtime.GOOS == osDarwin {
		return []Browser{
			BrowserSafari, BrowserChrome, BrowserFirefox, BrowserBrave,
			BrowserArc, BrowserEdge, BrowserHelium, BrowserOpera, BrowserVivaldi,
		}
	}
	return []Browser{
		BrowserChrome, BrowserChromium, BrowserFirefox, BrowserBrave,
		BrowserEdge, BrowserOpera, BrowserVivaldi,
	}
}

// IsChromiumBased returns true if the browser uses Chromium's cookie format.
func IsChromiumBased(browser Browser) bool {
	switch browser { //nolint:exhaustive // Non-Chromium browsers return false via default
	case BrowserChrome, BrowserChromium, BrowserBrave, BrowserEdge,
		BrowserArc, BrowserHelium, BrowserOpera, BrowserVivaldi:
		return true
	default:
		return false
	}
}

// ExtractLinkedInCookies extracts LinkedIn cookies from the specified browser.
func ExtractLinkedInCookies(browser Browser) (*api.Credentials, error) {
	var cookies []Cookie
	var err error

	switch browser {
	case BrowserSafari:
		if runtime.GOOS != osDarwin {
			return nil, errors.New("Safari is only available on macOS")
		}
		cookies, err = extractSafariCookies()

	case BrowserFirefox:
		if runtime.GOOS != osDarwin && runtime.GOOS != osLinux {
			return nil, fmt.Errorf("Firefox cookie extraction not supported on %s", runtime.GOOS)
		}
		cookies, err = extractFirefoxCookies()

	case BrowserChrome, BrowserChromium, BrowserBrave, BrowserEdge,
		BrowserArc, BrowserHelium, BrowserOpera, BrowserVivaldi:
		if runtime.GOOS != osDarwin && runtime.GOOS != osLinux {
			return nil, fmt.Errorf("%s cookie extraction not supported on %s", browser, runtime.GOOS)
		}
		cookies, err = extractChromiumCookies(browser)

	default:
		return nil, fmt.Errorf("unsupported browser: %s. Supported: %v", browser, SupportedBrowsers())
	}

	if err != nil {
		return nil, err
	}

	return cookiesToCredentials(cookies)
}

// cookiesToCredentials converts LinkedIn cookies to API credentials.
func cookiesToCredentials(cookies []Cookie) (*api.Credentials, error) {
	creds := &api.Credentials{}

	for _, c := range cookies {
		switch c.Name {
		case cookieLiAt:
			creds.LiAt = c.Value
			if !c.ExpiresAt.IsZero() {
				creds.ExpiresAt = c.ExpiresAt
			}
		case cookieJSessionID:
			creds.JSessID = c.Value
			// Extract CSRF token from JSESSIONID (remove quotes).
			creds.CSRFToken = strings.Trim(c.Value, `"`)
		}
	}

	if creds.LiAt == "" {
		return nil, errors.New(cookieLiAt + " cookie not found. Make sure you're logged into LinkedIn in your browser")
	}
	if creds.JSessID == "" {
		return nil, errors.New(cookieJSessionID + " cookie not found. Make sure you're logged into LinkedIn in your browser")
	}

	return creds, nil
}

// Safari cookie extraction.
// Safari stores cookies in ~/Library/Cookies/Cookies.binarycookies

func extractSafariCookies() ([]Cookie, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	cookiePath := filepath.Join(home, "Library", "Cookies", "Cookies.binarycookies")
	return parseBinaryCookies(cookiePath, "linkedin.com")
}

// parseBinaryCookies parses Safari's binary cookie format.
// Format documentation: https://github.com/libyal/dtformats/blob/main/documentation/Safari%20Cookies.asciidoc
func parseBinaryCookies(path, domainFilter string) ([]Cookie, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("Safari cookies file not found at %s", path)
		}
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied reading Safari cookies. Grant Full Disk Access to Terminal in System Preferences > Privacy & Security")
		}
		return nil, fmt.Errorf("failed to read cookies file: %w", err)
	}

	return parseBinaryCookiesData(data, domainFilter)
}

// parseBinaryCookiesData parses the binary cookie data.
func parseBinaryCookiesData(data []byte, domainFilter string) ([]Cookie, error) {
	if len(data) < 4 {
		return nil, errors.New("invalid cookie file: too short")
	}

	// Check magic bytes: "cook".
	if string(data[:4]) != "cook" {
		return nil, errors.New("invalid cookie file: bad magic bytes")
	}

	reader := bytes.NewReader(data[4:])

	// Read number of pages.
	var numPages uint32
	if err := binary.Read(reader, binary.BigEndian, &numPages); err != nil {
		return nil, fmt.Errorf("failed to read page count: %w", err)
	}

	// Read page sizes.
	pageSizes := make([]uint32, numPages)
	for i := uint32(0); i < numPages; i++ {
		if err := binary.Read(reader, binary.BigEndian, &pageSizes[i]); err != nil {
			return nil, fmt.Errorf("failed to read page size: %w", err)
		}
	}

	var cookies []Cookie

	// Read each page.
	for i := uint32(0); i < numPages; i++ {
		pageData := make([]byte, pageSizes[i])
		if _, err := reader.Read(pageData); err != nil {
			return nil, fmt.Errorf("failed to read page: %w", err)
		}

		pageCookies, err := parseCookiePage(pageData, domainFilter)
		if err != nil {
			// Skip invalid pages but continue.
			continue
		}
		cookies = append(cookies, pageCookies...)
	}

	return cookies, nil
}

// parseCookiePage parses a single page of cookies.
func parseCookiePage(data []byte, domainFilter string) ([]Cookie, error) {
	if len(data) < 8 {
		return nil, errors.New("page too short")
	}

	reader := bytes.NewReader(data)

	// Page header: 4 bytes (should be 0x00000100).
	var pageHeader uint32
	_ = binary.Read(reader, binary.LittleEndian, &pageHeader)

	// Number of cookies in page.
	var numCookies uint32
	_ = binary.Read(reader, binary.LittleEndian, &numCookies)

	// Read cookie offsets.
	offsets := make([]uint32, numCookies)
	for i := uint32(0); i < numCookies; i++ {
		_ = binary.Read(reader, binary.LittleEndian, &offsets[i])
	}

	var cookies []Cookie

	// Parse each cookie.
	for _, offset := range offsets {
		if int(offset) >= len(data) {
			continue
		}

		cookie, err := parseCookie(data[offset:], domainFilter)
		if err != nil {
			continue
		}
		if cookie != nil {
			cookies = append(cookies, *cookie)
		}
	}

	return cookies, nil
}

// parseCookie parses a single cookie from binary data.
func parseCookie(data []byte, domainFilter string) (*Cookie, error) {
	if len(data) < 48 {
		return nil, errors.New("cookie data too short")
	}

	reader := bytes.NewReader(data)

	// Cookie size.
	var cookieSize uint32
	_ = binary.Read(reader, binary.LittleEndian, &cookieSize)

	// Unknown field.
	var unknown1 uint32
	_ = binary.Read(reader, binary.LittleEndian, &unknown1)

	// Flags.
	var flags uint32
	_ = binary.Read(reader, binary.LittleEndian, &flags)

	// Unknown field.
	var unknown2 uint32
	_ = binary.Read(reader, binary.LittleEndian, &unknown2)

	// Offsets to strings.
	var domainOffset, nameOffset, pathOffset, valueOffset uint32
	_ = binary.Read(reader, binary.LittleEndian, &domainOffset)
	_ = binary.Read(reader, binary.LittleEndian, &nameOffset)
	_ = binary.Read(reader, binary.LittleEndian, &pathOffset)
	_ = binary.Read(reader, binary.LittleEndian, &valueOffset)

	// End of cookie (8 bytes).
	var endHeader uint64
	_ = binary.Read(reader, binary.LittleEndian, &endHeader)

	// Expiration date (Mac absolute time - seconds since 2001-01-01).
	var expiration float64
	_ = binary.Read(reader, binary.LittleEndian, &expiration)

	// Creation date.
	var creation float64
	_ = binary.Read(reader, binary.LittleEndian, &creation)

	// Read strings.
	domain := readNullTerminatedString(data, domainOffset)
	name := readNullTerminatedString(data, nameOffset)
	path := readNullTerminatedString(data, pathOffset)
	value := readNullTerminatedString(data, valueOffset)

	// Filter by domain.
	if domainFilter != "" && !strings.Contains(domain, domainFilter) {
		return nil, nil
	}

	// Convert Mac absolute time to Go time.
	// Mac absolute time starts at 2001-01-01 00:00:00 UTC.
	macEpoch := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	expiresAt := macEpoch.Add(time.Duration(expiration) * time.Second)

	cookie := &Cookie{
		Domain:     domain,
		Name:       name,
		Value:      value,
		Path:       path,
		ExpiresAt:  expiresAt,
		IsSecure:   flags&1 != 0,
		IsHTTPOnly: flags&4 != 0,
	}

	return cookie, nil
}

// readNullTerminatedString reads a null-terminated string from data at offset.
func readNullTerminatedString(data []byte, offset uint32) string {
	if int(offset) >= len(data) {
		return ""
	}

	end := offset
	for int(end) < len(data) && data[end] != 0 {
		end++
	}

	return string(data[offset:end])
}

// FromEnvironment creates credentials from environment variables.
// Supports LNK_LI_AT, LNK_JSESSIONID, or LNK_COOKIES (combined format).
func FromEnvironment() (*api.Credentials, error) {
	liAt := os.Getenv("LNK_LI_AT")
	jsessionID := os.Getenv("LNK_JSESSIONID")

	// Also check combined format.
	if combined := os.Getenv("LNK_COOKIES"); combined != "" {
		return parseCookieString(combined)
	}

	if liAt == "" || jsessionID == "" {
		return nil, errors.New("LNK_LI_AT and LNK_JSESSIONID environment variables not set")
	}

	return &api.Credentials{
		LiAt:      liAt,
		JSessID:   jsessionID,
		CSRFToken: strings.Trim(jsessionID, `"`),
	}, nil
}

// parseCookieString parses a cookie string like "li_at=xxx; JSESSIONID=yyy".
func parseCookieString(s string) (*api.Credentials, error) {
	creds := &api.Credentials{}

	parts := strings.Split(s, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "="); idx > 0 {
			name := strings.TrimSpace(part[:idx])
			value := strings.TrimSpace(part[idx+1:])

			switch name {
			case cookieLiAt:
				creds.LiAt = value
			case cookieJSessionID:
				creds.JSessID = value
				creds.CSRFToken = strings.Trim(value, `"`)
			}
		}
	}

	if creds.LiAt == "" || creds.JSessID == "" {
		return nil, errors.New("invalid cookie string: missing " + cookieLiAt + " or " + cookieJSessionID)
	}

	return creds, nil
}
