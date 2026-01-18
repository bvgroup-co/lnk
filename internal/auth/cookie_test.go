package auth

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestSupportedBrowsers(t *testing.T) {
	browsers := SupportedBrowsers()

	// Chrome and Firefox should always be present.
	hasChrome := false
	hasFirefox := false
	hasSafari := false

	for _, b := range browsers {
		switch b { //nolint:exhaustive // Only checking specific browsers
		case BrowserChrome:
			hasChrome = true
		case BrowserFirefox:
			hasFirefox = true
		case BrowserSafari:
			hasSafari = true
		}
	}

	if !hasChrome {
		t.Error("Chrome should be in supported browsers")
	}
	if !hasFirefox {
		t.Error("Firefox should be in supported browsers")
	}

	// Safari only on macOS.
	if runtime.GOOS == osDarwin && !hasSafari {
		t.Error("Safari should be in supported browsers on macOS")
	}
	if runtime.GOOS != osDarwin && hasSafari {
		t.Error("Safari should not be in supported browsers on non-macOS")
	}
}

func TestCookiesToCredentials(t *testing.T) {
	tests := []struct {
		name    string
		cookies []Cookie
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty cookies",
			cookies: []Cookie{},
			wantErr: true,
			errMsg:  "li_at cookie not found",
		},
		{
			name: "missing JSESSIONID",
			cookies: []Cookie{
				{Name: "li_at", Value: "test-token"},
			},
			wantErr: true,
			errMsg:  "JSESSIONID cookie not found",
		},
		{
			name: "valid cookies",
			cookies: []Cookie{
				{Name: "li_at", Value: "test-li-at"},
				{Name: "JSESSIONID", Value: `"test-session"`},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := cookiesToCredentials(tt.cookies)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("error message = %q, want contains %q", err.Error(), tt.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.LiAt != "test-li-at" {
				t.Errorf("LiAt = %q, want %q", creds.LiAt, "test-li-at")
			}
			if creds.JSessID != `"test-session"` {
				t.Errorf("JSessID = %q, want %q", creds.JSessID, `"test-session"`)
			}
			if creds.CSRFToken != "test-session" {
				t.Errorf("CSRFToken = %q, want %q", creds.CSRFToken, "test-session")
			}
		})
	}
}

func TestFromEnvironment(t *testing.T) {
	// Clear environment.
	os.Unsetenv("LNK_LI_AT")
	os.Unsetenv("LNK_JSESSIONID")
	os.Unsetenv("LNK_COOKIES")

	// Test missing environment variables.
	_, err := FromEnvironment()
	if err == nil {
		t.Error("expected error when environment variables not set")
	}

	// Test with LNK_LI_AT and LNK_JSESSIONID.
	os.Setenv("LNK_LI_AT", "test-li-at")
	os.Setenv("LNK_JSESSIONID", `"test-session"`)
	defer func() {
		os.Unsetenv("LNK_LI_AT")
		os.Unsetenv("LNK_JSESSIONID")
	}()

	creds, err := FromEnvironment()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LiAt != "test-li-at" {
		t.Errorf("LiAt = %q, want %q", creds.LiAt, "test-li-at")
	}
	if creds.JSessID != `"test-session"` {
		t.Errorf("JSessID = %q, want %q", creds.JSessID, `"test-session"`)
	}
	if creds.CSRFToken != "test-session" {
		t.Errorf("CSRFToken = %q, want %q", creds.CSRFToken, "test-session")
	}
}

func TestFromEnvironmentCombined(t *testing.T) {
	os.Unsetenv("LNK_LI_AT")
	os.Unsetenv("LNK_JSESSIONID")
	os.Setenv("LNK_COOKIES", `li_at=combined-li-at; JSESSIONID="combined-session"`)
	defer os.Unsetenv("LNK_COOKIES")

	creds, err := FromEnvironment()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if creds.LiAt != "combined-li-at" {
		t.Errorf("LiAt = %q, want %q", creds.LiAt, "combined-li-at")
	}
	if creds.JSessID != `"combined-session"` {
		t.Errorf("JSessID = %q, want %q", creds.JSessID, `"combined-session"`)
	}
}

func TestParseCookieString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid cookie string",
			input:   `li_at=token123; JSESSIONID="session456"`,
			wantErr: false,
		},
		{
			name:    "with extra spaces",
			input:   `  li_at = token123 ;  JSESSIONID = "session456"  `,
			wantErr: false,
		},
		{
			name:    "missing li_at",
			input:   `JSESSIONID="session"`,
			wantErr: true,
		},
		{
			name:    "missing JSESSIONID",
			input:   `li_at=token`,
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds, err := parseCookieString(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if creds.LiAt == "" {
				t.Error("LiAt is empty")
			}
			if creds.JSessID == "" {
				t.Error("JSessID is empty")
			}
		})
	}
}

func TestReadNullTerminatedString(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		offset uint32
		want   string
	}{
		{
			name:   "normal string",
			data:   []byte("hello\x00world"),
			offset: 0,
			want:   "hello",
		},
		{
			name:   "string at offset",
			data:   []byte("hello\x00world\x00"),
			offset: 6,
			want:   "world",
		},
		{
			name:   "offset beyond data",
			data:   []byte("hello"),
			offset: 100,
			want:   "",
		},
		{
			name:   "no null terminator",
			data:   []byte("hello"),
			offset: 0,
			want:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readNullTerminatedString(tt.data, tt.offset)
			if got != tt.want {
				t.Errorf("readNullTerminatedString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractLinkedInCookiesSafariNonMac(t *testing.T) {
	if runtime.GOOS == "osDarwin" {
		t.Skip("skipping on macOS")
	}

	_, err := ExtractLinkedInCookies(BrowserSafari)
	if err == nil {
		t.Error("expected error for Safari on non-macOS")
	}
}

func TestExtractLinkedInCookiesUnsupportedBrowser(t *testing.T) {
	_, err := ExtractLinkedInCookies(Browser("invalid"))
	if err == nil {
		t.Error("expected error for unsupported browser")
	}
}
