package auth

import (
	"bytes"
	"testing"
	"time"
)

func TestChromeTimeToUnix(t *testing.T) {
	tests := []struct {
		name       string
		chromeTime int64
		wantZero   bool
	}{
		{
			name:       "zero time",
			chromeTime: 0,
			wantZero:   true,
		},
		{
			name:       "valid time",
			chromeTime: 13337000000000000, // Some time in 2023
			wantZero:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chromeTimeToUnix(tt.chromeTime)
			if tt.wantZero && !result.IsZero() {
				t.Error("expected zero time")
			}
			if !tt.wantZero && result.IsZero() {
				t.Error("expected non-zero time")
			}
		})
	}
}

func TestRemovePKCS7Padding(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  []byte
	}{
		{
			name:  "empty",
			input: []byte{},
			want:  []byte{},
		},
		{
			name:  "valid padding",
			input: []byte("hello\x03\x03\x03"),
			want:  []byte("hello"),
		},
		{
			name:  "single byte padding",
			input: []byte("hello\x01"),
			want:  []byte("hello"),
		},
		{
			name:  "full block padding",
			input: append([]byte("hello"), repeatByte(16, 16)...),
			want:  []byte("hello"),
		},
		{
			name:  "invalid padding (padding byte > len)",
			input: []byte{0x10},
			want:  []byte{0x10},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removePKCS7Padding(tt.input)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("removePKCS7Padding() = %v, want %v", got, tt.want)
			}
		})
	}
}

// repeatByte creates a slice of n bytes with value v.
func repeatByte(n int, v byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = v
	}
	return b
}

func TestDecryptChromeCookie(t *testing.T) {
	// Test with empty input.
	result, err := decryptChromeCookie([]byte{}, []byte("key"))
	if err != nil {
		t.Errorf("unexpected error for empty input: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result for empty input, got %q", result)
	}

	// Test with unencrypted value (no version prefix).
	result, err = decryptChromeCookie([]byte("plaintext"), []byte("key"))
	if err != nil {
		t.Errorf("unexpected error for plaintext: %v", err)
	}
	if result != "plaintext" {
		t.Errorf("expected %q, got %q", "plaintext", result)
	}
}

func TestChromeTimeConversion(t *testing.T) {
	// Test a known Chrome timestamp.
	// Chrome time: 13337000000000000 should be around 2023.
	chromeTime := int64(13337000000000000)
	unixTime := chromeTimeToUnix(chromeTime)

	// Should be a valid time in the past.
	if unixTime.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("converted time is too old")
	}
	if unixTime.After(time.Now()) {
		t.Error("converted time is in the future")
	}
}
