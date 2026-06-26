// Package auth provides authentication functionality for LinkedIn.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bvgroup-co/lnk/internal/api"
)

const (
	// ConfigDir is the directory name for lnk config.
	ConfigDir = "lnk"
	// CredentialsFile is the filename for stored credentials.
	CredentialsFile = "credentials.json"
)

// Store manages credential storage.
type Store struct {
	configDir string
}

// NewStore creates a new credential store.
func NewStore() (*Store, error) {
	configDir, err := getConfigDir()
	if err != nil {
		return nil, err
	}
	return &Store{configDir: configDir}, nil
}

// getConfigDir returns the configuration directory path.
func getConfigDir() (string, error) {
	// Use XDG_CONFIG_HOME if set, otherwise ~/.config.
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, ConfigDir), nil
}

// Save stores credentials to disk.
func (s *Store) Save(creds *api.Credentials) error {
	// Ensure config directory exists.
	if err := os.MkdirAll(s.configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal credentials.
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal credentials: %w", err)
	}

	// Write to file with restricted permissions.
	credPath := filepath.Join(s.configDir, CredentialsFile)
	if err := os.WriteFile(credPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write credentials: %w", err)
	}

	return nil
}

// Load retrieves stored credentials.
func (s *Store) Load() (*api.Credentials, error) {
	credPath := filepath.Join(s.configDir, CredentialsFile)

	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoCredentials
		}
		return nil, fmt.Errorf("failed to read credentials: %w", err)
	}

	var creds api.Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	return &creds, nil
}

// Delete removes stored credentials.
func (s *Store) Delete() error {
	credPath := filepath.Join(s.configDir, CredentialsFile)

	if err := os.Remove(credPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted.
		}
		return fmt.Errorf("failed to delete credentials: %w", err)
	}

	return nil
}

// Exists checks if credentials are stored.
func (s *Store) Exists() bool {
	credPath := filepath.Join(s.configDir, CredentialsFile)
	_, err := os.Stat(credPath)
	return err == nil
}

// Path returns the credentials file path.
func (s *Store) Path() string {
	return filepath.Join(s.configDir, CredentialsFile)
}

// ErrNoCredentials indicates no stored credentials exist.
var ErrNoCredentials = errors.New("no stored credentials")
