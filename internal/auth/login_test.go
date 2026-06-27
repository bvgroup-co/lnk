package auth

import (
	"strings"
	"testing"
)

func TestLoginWithCredentialsOptionsRejectsUnsupportedProxyScheme(t *testing.T) {
	_, err := LoginWithCredentialsOptions("user@example.com", "password", WithProxyURL("ftp://proxy.example:21"))
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
	if !strings.Contains(err.Error(), `unsupported scheme "ftp"`) {
		t.Fatalf("error = %q, want unsupported proxy scheme", err.Error())
	}
}

func TestLoginWithCredentialsOptionsRedactsInvalidProxyUserinfo(t *testing.T) {
	_, err := LoginWithCredentialsOptions("user@example.com", "password", WithProxyURL("http://proxy-user:proxy-secret@"))
	if err == nil {
		t.Fatal("expected invalid proxy error")
	}
	for _, secret := range []string{"proxy-user", "proxy-secret"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error leaked %q: %v", secret, err)
		}
	}
}
