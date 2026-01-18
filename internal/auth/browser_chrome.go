package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1" //nolint:gosec // Required for Chrome's PBKDF2 implementation
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/pbkdf2"
)

// chromiumBrowserConfig holds configuration for a Chromium-based browser.
type chromiumBrowserConfig struct {
	name            string
	macOSPath       string
	linuxPath       string
	keychainService string
	keychainAccount string
}

// getChromiumConfig returns the configuration for a Chromium-based browser.
func getChromiumConfig(browser Browser) chromiumBrowserConfig {
	switch browser { //nolint:exhaustive // Only Chromium browsers handled here
	case BrowserChrome:
		return chromiumBrowserConfig{
			name:            "Chrome",
			macOSPath:       "Google/Chrome",
			linuxPath:       "google-chrome",
			keychainService: "Chrome Safe Storage",
			keychainAccount: "Chrome",
		}
	case BrowserChromium:
		return chromiumBrowserConfig{
			name:            "Chromium",
			macOSPath:       "Chromium",
			linuxPath:       "chromium",
			keychainService: "Chromium Safe Storage",
			keychainAccount: "Chromium",
		}
	case BrowserBrave:
		return chromiumBrowserConfig{
			name:            "Brave",
			macOSPath:       "BraveSoftware/Brave-Browser",
			linuxPath:       "BraveSoftware/Brave-Browser",
			keychainService: "Brave Safe Storage",
			keychainAccount: "Brave",
		}
	case BrowserEdge:
		return chromiumBrowserConfig{
			name:            "Edge",
			macOSPath:       "Microsoft Edge",
			linuxPath:       "microsoft-edge",
			keychainService: "Microsoft Edge Safe Storage",
			keychainAccount: "Microsoft Edge",
		}
	case BrowserArc:
		return chromiumBrowserConfig{
			name:            "Arc",
			macOSPath:       "Arc",
			linuxPath:       "", // Arc is macOS only
			keychainService: "Arc Safe Storage",
			keychainAccount: "Arc",
		}
	case BrowserHelium:
		return chromiumBrowserConfig{
			name:            "Helium",
			macOSPath:       "net.imput.helium",
			linuxPath:       "helium",
			keychainService: "Helium Storage Key",
			keychainAccount: "Helium",
		}
	case BrowserOpera:
		return chromiumBrowserConfig{
			name:            "Opera",
			macOSPath:       "com.operasoftware.Opera",
			linuxPath:       "opera",
			keychainService: "Opera Safe Storage",
			keychainAccount: "Opera",
		}
	case BrowserVivaldi:
		return chromiumBrowserConfig{
			name:            "Vivaldi",
			macOSPath:       "Vivaldi",
			linuxPath:       "vivaldi",
			keychainService: "Vivaldi Safe Storage",
			keychainAccount: "Vivaldi",
		}
	default:
		return chromiumBrowserConfig{
			name:            "Chrome",
			macOSPath:       "Google/Chrome",
			linuxPath:       "google-chrome",
			keychainService: "Chrome Safe Storage",
			keychainAccount: "Chrome",
		}
	}
}

// extractChromiumCookies extracts LinkedIn cookies from a Chromium-based browser.
func extractChromiumCookies(browser Browser) ([]Cookie, error) {
	config := getChromiumConfig(browser)

	cookiePath, err := findChromiumCookiesPath(&config)
	if err != nil {
		return nil, err
	}

	// Browser may lock the database, so copy it to a temp file.
	tmpFile, err := copyToTemp(cookiePath)
	if err != nil {
		return nil, fmt.Errorf("failed to copy cookies database: %w", err)
	}
	defer os.Remove(tmpFile)

	// Get decryption key.
	key, err := getChromiumDecryptionKey(&config)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s decryption key: %w", config.name, err)
	}

	return readChromiumCookies(tmpFile, key, config.name)
}

// findChromiumCookiesPath locates the cookies database for a Chromium-based browser.
func findChromiumCookiesPath(config *chromiumBrowserConfig) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	var basePath string

	switch runtime.GOOS {
	case osDarwin:
		basePath = filepath.Join(home, "Library", "Application Support", config.macOSPath)
	case osLinux:
		if config.linuxPath == "" {
			return "", fmt.Errorf("%s is not available on Linux", config.name)
		}
		basePath = filepath.Join(home, ".config", config.linuxPath)
	default:
		return "", fmt.Errorf("%s cookie extraction not supported on %s", config.name, runtime.GOOS)
	}

	// Check if browser directory exists.
	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return "", fmt.Errorf("%s not found. Is it installed?", config.name)
	}

	// Check Default profile first.
	cookiePath := filepath.Join(basePath, "Default", "Cookies")
	if _, err := os.Stat(cookiePath); err == nil {
		return cookiePath, nil
	}

	// Try Network/Cookies (newer versions).
	networkCookiePath := filepath.Join(basePath, "Default", "Network", "Cookies")
	if _, err := os.Stat(networkCookiePath); err == nil {
		return networkCookiePath, nil
	}

	return "", fmt.Errorf("%s cookies database not found", config.name)
}

// getChromiumDecryptionKey retrieves the key used to decrypt cookies.
func getChromiumDecryptionKey(config *chromiumBrowserConfig) ([]byte, error) {
	switch runtime.GOOS {
	case osDarwin:
		return getChromiumKeyMacOS(config)
	case osLinux:
		return getChromiumKeyLinux(config)
	default:
		return nil, fmt.Errorf("decryption not supported on %s", runtime.GOOS)
	}
}

// getChromiumKeyMacOS retrieves the encryption key from macOS Keychain.
func getChromiumKeyMacOS(config *chromiumBrowserConfig) ([]byte, error) {
	// Use security command to get the key from Keychain.
	cmd := exec.Command("security", "find-generic-password", //nolint:gosec // Args are from trusted config
		"-w", // Print password only
		"-s", config.keychainService,
		"-a", config.keychainAccount,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get %s key from Keychain: %w", config.name, err)
	}

	password := strings.TrimSpace(string(output))

	// Derive key using PBKDF2.
	salt := []byte("saltysalt")
	key := pbkdf2.Key([]byte(password), salt, 1003, 16, sha1.New)

	return key, nil
}

// getChromiumKeyLinux retrieves the encryption key on Linux.
func getChromiumKeyLinux(config *chromiumBrowserConfig) ([]byte, error) {
	// On Linux, try GNOME Keyring first, then fallback to hardcoded key.
	cmd := exec.Command("secret-tool", "lookup", //nolint:gosec // Args are from trusted config
		"application", strings.ToLower(config.name),
	)

	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		password := strings.TrimSpace(string(output))
		salt := []byte("saltysalt")
		key := pbkdf2.Key([]byte(password), salt, 1, 16, sha1.New)
		return key, nil
	}

	// Fallback to hardcoded key.
	salt := []byte("saltysalt")
	key := pbkdf2.Key([]byte("peanuts"), salt, 1, 16, sha1.New)

	return key, nil
}

// readChromiumCookies reads and decrypts cookies from a Chromium cookies database.
func readChromiumCookies(dbPath string, key []byte, browserName string) ([]Cookie, error) {
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open cookies database: %w", err)
	}
	defer db.Close()

	// Query LinkedIn cookies.
	query := `
		SELECT name, encrypted_value, host_key, path, expires_utc, is_secure, is_httponly
		FROM cookies
		WHERE host_key LIKE '%linkedin.com'
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cookies: %w", err)
	}
	defer rows.Close()

	var cookies []Cookie
	for rows.Next() {
		var name, host, path string
		var encryptedValue []byte
		var expiresUTC int64
		var isSecure, isHTTPOnly int

		if err := rows.Scan(&name, &encryptedValue, &host, &path, &expiresUTC, &isSecure, &isHTTPOnly); err != nil {
			continue
		}

		// Decrypt cookie value.
		value, err := decryptChromeCookie(encryptedValue, key)
		if err != nil {
			// Try unencrypted value.
			value = string(encryptedValue)
		}

		// Chrome stores time as microseconds since 1601-01-01.
		expiresAt := chromeTimeToUnix(expiresUTC)

		cookies = append(cookies, Cookie{
			Domain:     host,
			Name:       name,
			Value:      value,
			Path:       path,
			ExpiresAt:  expiresAt,
			IsSecure:   isSecure == 1,
			IsHTTPOnly: isHTTPOnly == 1,
		})
	}

	if len(cookies) == 0 {
		return nil, fmt.Errorf("no LinkedIn cookies found in %s. Make sure you're logged into LinkedIn", browserName)
	}

	return cookies, nil
}

// decryptChromeCookie decrypts a Chrome cookie value.
func decryptChromeCookie(encrypted, key []byte) (string, error) {
	if len(encrypted) == 0 {
		return "", nil
	}

	// Check for encryption version prefix.
	if runtime.GOOS == osDarwin && len(encrypted) > 3 && string(encrypted[:3]) == "v10" {
		// v10 encryption (AES-128-CBC).
		return decryptV10Cookie(encrypted[3:], key)
	}

	if runtime.GOOS == osLinux && len(encrypted) > 3 && string(encrypted[:3]) == "v11" {
		// v11 encryption (AES-128-CBC).
		return decryptV10Cookie(encrypted[3:], key)
	}

	if runtime.GOOS == osLinux && len(encrypted) > 3 && string(encrypted[:3]) == "v10" {
		return decryptV10Cookie(encrypted[3:], key)
	}

	// Unencrypted or unknown format.
	return string(encrypted), nil
}

// decryptV10Cookie decrypts a v10 encrypted cookie using AES-128-CBC.
func decryptV10Cookie(encrypted, key []byte) (string, error) {
	if len(encrypted) < aes.BlockSize {
		return "", fmt.Errorf("encrypted data too short")
	}

	// IV is all spaces for Chrome.
	iv := []byte("                ") // 16 spaces

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	mode := cipher.NewCBCDecrypter(block, iv)

	// Ensure encrypted data is multiple of block size.
	if len(encrypted)%aes.BlockSize != 0 {
		return "", fmt.Errorf("encrypted data not aligned")
	}

	decrypted := make([]byte, len(encrypted))
	mode.CryptBlocks(decrypted, encrypted)

	// Remove PKCS7 padding.
	decrypted = removePKCS7Padding(decrypted)

	return string(decrypted), nil
}

// removePKCS7Padding removes PKCS7 padding from decrypted data.
func removePKCS7Padding(data []byte) []byte {
	if len(data) == 0 {
		return data
	}
	padding := int(data[len(data)-1])
	if padding > len(data) || padding > aes.BlockSize {
		return data
	}
	// Verify padding bytes.
	for i := len(data) - padding; i < len(data); i++ {
		if data[i] != byte(padding) {
			return data
		}
	}
	return data[:len(data)-padding]
}

// chromeTimeToUnix converts Chrome's timestamp format to Unix time.
// Chrome uses microseconds since 1601-01-01.
func chromeTimeToUnix(chromeTime int64) time.Time {
	if chromeTime == 0 {
		return time.Time{}
	}
	// Microseconds between 1601-01-01 and 1970-01-01.
	const epochDiff = 11644473600000000
	unixMicro := chromeTime - epochDiff
	return time.Unix(unixMicro/1000000, (unixMicro%1000000)*1000)
}
