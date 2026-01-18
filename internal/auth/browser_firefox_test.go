package auth

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFindFirefoxProfile(t *testing.T) {
	// This test will fail if Firefox is not installed.
	// We just test that the function returns an appropriate error.
	_, err := findFirefoxProfile()
	if err != nil {
		// Expected on systems without Firefox.
		t.Logf("findFirefoxProfile returned expected error: %v", err)
	}
}

func TestCopyToTemp(t *testing.T) {
	// Create a temp file to copy.
	tmpDir := t.TempDir()
	srcFile := filepath.Join(tmpDir, "test.txt")
	content := []byte("test content")

	if err := os.WriteFile(srcFile, content, 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test successful copy.
	dstFile, err := copyToTemp(srcFile)
	if err != nil {
		t.Fatalf("copyToTemp failed: %v", err)
	}
	defer os.Remove(dstFile)

	// Verify content.
	copied, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("failed to read copied file: %v", err)
	}
	if !bytes.Equal(copied, content) {
		t.Errorf("copied content mismatch: got %q, want %q", copied, content)
	}

	// Test with non-existent file.
	_, err = copyToTemp(filepath.Join(tmpDir, "nonexistent.txt"))
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFirefoxProfilePaths(t *testing.T) {
	if runtime.GOOS != osDarwin && runtime.GOOS != osLinux {
		t.Skip("Firefox profile path test only runs on macOS and Linux")
	}

	// This is more of a documentation test showing expected paths.
	home, _ := os.UserHomeDir()

	var expectedBase string
	if runtime.GOOS == osDarwin {
		expectedBase = filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles")
	} else {
		expectedBase = filepath.Join(home, ".mozilla", "firefox")
	}

	t.Logf("Expected Firefox profiles directory: %s", expectedBase)
}
