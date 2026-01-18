// Package commands provides CLI command implementations.
package commands

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/pp/lnk/internal/api"
	"github.com/pp/lnk/internal/auth"
)

var (
	authBrowser    string
	authEmail      string
	authPassword   string
	authLiAt       string
	authJSessionID string
)

// NewAuthCmd creates the auth command group.
func NewAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
		Long:  `Commands for authenticating with LinkedIn.`,
	}

	cmd.AddCommand(newAuthLoginCmd())
	cmd.AddCommand(newAuthStatusCmd())
	cmd.AddCommand(newAuthLogoutCmd())

	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with LinkedIn",
		Long: `Authenticate with LinkedIn.

Email/Password (interactive):
  lnk auth login --email user@example.com
  (You will be prompted for your password securely)

Direct cookie entry:
  lnk auth login --li-at "YOUR_LI_AT" --jsessionid "YOUR_JSESSIONID"

Browser cookie extraction:
  lnk auth login --browser safari
  lnk auth login --browser chrome

Environment variables:
  Set LNK_LI_AT and LNK_JSESSIONID, then run:
  lnk auth login --env

Note: Email/password auth may fail if you have 2FA enabled or if
LinkedIn requires captcha verification. In that case, use cookie auth.`,
		RunE: runAuthLogin,
	}

	cmd.Flags().StringVarP(&authEmail, "email", "e", "", "LinkedIn email address")
	cmd.Flags().StringVarP(&authPassword, "password", "p", "", "LinkedIn password (will prompt if not provided)")
	cmd.Flags().StringVar(&authLiAt, "li-at", "", "LinkedIn li_at cookie value")
	cmd.Flags().StringVar(&authJSessionID, "jsessionid", "", "LinkedIn JSESSIONID cookie value")
	cmd.Flags().StringVarP(&authBrowser, "browser", "b", "", "Browser to extract cookies from")
	cmd.Flags().Bool("env", false, "Use environment variables for authentication")

	return cmd
}

func runAuthLogin(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	useEnv, _ := cmd.Flags().GetBool("env")

	var creds *api.Credentials
	var err error
	var browserUsed auth.Browser

	switch {
	case authEmail != "":
		// Email/password authentication.
		password := authPassword
		if password == "" {
			// Prompt for password securely.
			if jsonOutput {
				return outputError(jsonOutput, "PASSWORD_REQUIRED", "password required for email auth in JSON mode. Use --password flag")
			}
			password, err = promptPassword("Password: ")
			if err != nil {
				return outputError(jsonOutput, "INPUT_ERROR", fmt.Sprintf("failed to read password: %v", err))
			}
		}
		if !jsonOutput {
			fmt.Println("Authenticating with LinkedIn...")
		}
		creds, err = auth.LoginWithCredentials(authEmail, password)

	case authLiAt != "" && authJSessionID != "":
		// Direct cookie entry via flags.
		creds = &api.Credentials{
			LiAt:      authLiAt,
			JSessID:   authJSessionID,
			CSRFToken: strings.Trim(authJSessionID, `"`),
		}

	case useEnv:
		creds, err = auth.FromEnvironment()

	case authBrowser != "":
		browserUsed = auth.Browser(strings.ToLower(authBrowser))
		creds, err = auth.ExtractLinkedInCookies(browserUsed)

	default:
		// No auth method specified - prompt for email interactively.
		if jsonOutput {
			return outputError(jsonOutput, "AUTH_METHOD_REQUIRED",
				"specify auth method: --email, --li-at/--jsessionid, --browser, or --env")
		}

		var email string
		email, err = promptInput("Email: ")
		if err != nil {
			return outputError(jsonOutput, "INPUT_ERROR", fmt.Sprintf("failed to read email: %v", err))
		}

		var password string
		password, err = promptPassword("Password: ")
		if err != nil {
			return outputError(jsonOutput, "INPUT_ERROR", fmt.Sprintf("failed to read password: %v", err))
		}

		fmt.Println("Authenticating with LinkedIn...")
		creds, err = auth.LoginWithCredentials(email, password)
		if err != nil {
			return outputError(jsonOutput, "LOGIN_FAILED", err.Error())
		}
	}

	if err != nil {
		return outputError(jsonOutput, "LOGIN_FAILED", err.Error())
	}

	// Validate credentials by checking if they look valid.
	if !creds.IsValid() {
		return outputError(jsonOutput, "INVALID_CREDENTIALS", "extracted credentials are invalid or expired")
	}

	// Store credentials.
	store, err := auth.NewStore()
	if err != nil {
		return outputError(jsonOutput, "STORE_ERROR", err.Error())
	}

	if err := store.Save(creds); err != nil {
		return outputError(jsonOutput, "STORE_ERROR", err.Error())
	}

	if jsonOutput {
		return outputJSON(api.Response[map[string]any]{
			Success: true,
			Data: map[string]any{
				"message":    "Successfully authenticated",
				"storedAt":   store.Path(),
				"hasLiAt":    creds.LiAt != "",
				"hasJSessID": creds.JSessID != "",
			},
		})
	}

	fmt.Println("Successfully authenticated with LinkedIn!")
	fmt.Printf("Credentials stored at: %s\n", store.Path())
	return nil
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check authentication status",
		Long:  `Check if you are currently authenticated with LinkedIn.`,
		RunE:  runAuthStatus,
	}
}

func runAuthStatus(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	store, err := auth.NewStore()
	if err != nil {
		return outputError(jsonOutput, "STORE_ERROR", err.Error())
	}

	creds, err := store.Load()
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			if jsonOutput {
				return outputJSON(api.Response[map[string]any]{
					Success: true,
					Data: map[string]any{
						"authenticated": false,
						"message":       "Not authenticated. Run: lnk auth login",
					},
				})
			}
			fmt.Println("Not authenticated.")
			fmt.Println("Run: lnk auth login --browser safari")
			return nil
		}
		return outputError(jsonOutput, "STORE_ERROR", err.Error())
	}

	isValid := creds.IsValid()

	if jsonOutput {
		data := map[string]any{
			"authenticated": true,
			"valid":         isValid,
			"hasLiAt":       creds.LiAt != "",
			"hasJSessID":    creds.JSessID != "",
			"storedAt":      store.Path(),
		}
		if !creds.ExpiresAt.IsZero() {
			data["expiresAt"] = creds.ExpiresAt.Format("2006-01-02T15:04:05Z07:00")
		}
		return outputJSON(api.Response[map[string]any]{
			Success: true,
			Data:    data,
		})
	}

	if isValid {
		fmt.Println("Authenticated with LinkedIn.")
		fmt.Printf("Credentials stored at: %s\n", store.Path())
		if !creds.ExpiresAt.IsZero() {
			fmt.Printf("Expires: %s\n", creds.ExpiresAt.Format("2006-01-02 15:04:05"))
		}
	} else {
		fmt.Println("Credentials are expired or invalid.")
		fmt.Println("Run: lnk auth login --browser safari")
	}

	return nil
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Long:  `Remove stored LinkedIn credentials.`,
		RunE:  runAuthLogout,
	}
}

func runAuthLogout(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")

	store, err := auth.NewStore()
	if err != nil {
		return outputError(jsonOutput, "STORE_ERROR", err.Error())
	}

	if err := store.Delete(); err != nil {
		return outputError(jsonOutput, "STORE_ERROR", err.Error())
	}

	if jsonOutput {
		return outputJSON(api.Response[map[string]any]{
			Success: true,
			Data: map[string]any{
				"message": "Successfully logged out",
			},
		})
	}

	fmt.Println("Successfully logged out.")
	return nil
}

// Helper functions for output formatting.

func outputJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func outputError(jsonOutput bool, code, message string) error {
	if jsonOutput {
		_ = outputJSON(api.Response[any]{
			Success: false,
			Error: &api.Error{
				Code:    code,
				Message: message,
			},
		})
		os.Exit(1)
	}
	return fmt.Errorf("%s", message)
}

// promptInput prompts the user for text input.
func promptInput(prompt string) (string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(input), nil
}

// promptPassword prompts the user for password input without echoing.
func promptPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	password, err := term.ReadPassword(syscall.Stdin)
	fmt.Println() // Print newline after password input.
	if err != nil {
		return "", err
	}
	return string(password), nil
}
