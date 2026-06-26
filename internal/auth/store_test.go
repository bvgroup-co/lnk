package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bvgroup-co/lnk/internal/api"
)

func TestStore(t *testing.T) {
	// Create temp directory for tests.
	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	// Test Exists when no credentials.
	if store.Exists() {
		t.Error("Exists() should return false when no credentials stored")
	}

	// Test Load when no credentials.
	_, err = store.Load()
	if !errors.Is(err, ErrNoCredentials) {
		t.Errorf("Load() expected ErrNoCredentials, got: %v", err)
	}

	// Test Save.
	creds := &api.Credentials{
		LiAt:      "test-li-at-token",
		JSessID:   `"test-jsession-id"`,
		CSRFToken: "test-jsession-id",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}

	if saveErr := store.Save(creds); saveErr != nil {
		t.Fatalf("Save() error: %v", saveErr)
	}

	// Verify file permissions.
	credPath := store.Path()
	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("expected file permissions 0600, got %o", info.Mode().Perm())
	}

	// Test Exists after save.
	if !store.Exists() {
		t.Error("Exists() should return true after Save()")
	}

	// Test Load.
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.LiAt != creds.LiAt {
		t.Errorf("LiAt mismatch: got %q, want %q", loaded.LiAt, creds.LiAt)
	}
	if loaded.JSessID != creds.JSessID {
		t.Errorf("JSessID mismatch: got %q, want %q", loaded.JSessID, creds.JSessID)
	}
	if loaded.CSRFToken != creds.CSRFToken {
		t.Errorf("CSRFToken mismatch: got %q, want %q", loaded.CSRFToken, creds.CSRFToken)
	}

	// Test Delete.
	if err := store.Delete(); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if store.Exists() {
		t.Error("Exists() should return false after Delete()")
	}

	// Test Delete when already deleted (should not error).
	if err := store.Delete(); err != nil {
		t.Errorf("Delete() should not error when already deleted: %v", err)
	}
}

func TestStorePath(t *testing.T) {
	tmpDir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer os.Unsetenv("XDG_CONFIG_HOME")

	store, err := NewStore()
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}

	expected := filepath.Join(tmpDir, ConfigDir, CredentialsFile)
	if store.Path() != expected {
		t.Errorf("Path() = %q, want %q", store.Path(), expected)
	}
}

func TestGetConfigDirDefault(t *testing.T) {
	// Temporarily clear XDG_CONFIG_HOME.
	original := os.Getenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", original)

	configDir, err := getConfigDir()
	if err != nil {
		t.Fatalf("getConfigDir() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".config", ConfigDir)
	if configDir != expected {
		t.Errorf("getConfigDir() = %q, want %q", configDir, expected)
	}
}
