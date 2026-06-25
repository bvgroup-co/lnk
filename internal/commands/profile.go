package commands

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pp/lnk/internal/api"
	"github.com/pp/lnk/internal/auth"
)

var (
	profileActivityLimit    int
	profileActivityCategory string
	profileURN              string
)

// NewProfileCmd creates the profile command group.
func NewProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "View LinkedIn profiles",
		Long:  `Commands for viewing LinkedIn profiles.`,
	}

	cmd.AddCommand(newProfileMeCmd())
	cmd.AddCommand(newProfileGetCmd())
	cmd.AddCommand(newProfileActivityCmd())

	return cmd
}

func newProfileMeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "View your own profile",
		Long:  `Fetch and display your LinkedIn profile.`,
		RunE:  runProfileMe,
	}
}

func runProfileMe(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	profile, err := client.GetMyProfile(ctx)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	return outputProfile(jsonOutput, profile)
}

func newProfileGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get [username]",
		Short: "View a profile by username",
		Long: `Fetch and display a LinkedIn profile by username (public identifier).

Examples:
  lnk profile get johndoe
  lnk profile get --urn "urn:li:member:123456"`,
		Args: cobra.MaximumNArgs(1),
		RunE: runProfileGet,
	}

	cmd.Flags().StringVar(&profileURN, "urn", "", "Profile URN (alternative to username)")

	return cmd
}

func runProfileGet(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	// Validate input.
	if len(args) == 0 && profileURN == "" {
		return outputError(jsonOutput, api.ErrCodeInvalidInput, "provide a username or --urn")
	}

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	var profile *api.Profile

	if profileURN != "" {
		profile, err = client.GetProfileByURN(ctx, profileURN)
	} else {
		profile, err = client.GetProfile(ctx, args[0])
	}

	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	return outputProfile(jsonOutput, profile)
}

func newProfileActivityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity <username>",
		Short: "View recent profile activity",
		Long: `Fetch and display recent LinkedIn activity by username.

Examples:
  lnk profile activity johndoe
  lnk profile activity johndoe --category images --json
  lnk profile activity johndoe --limit 20
  lnk profile activity johndoe --json`,
		Args: cobra.ExactArgs(1),
		RunE: runProfileActivity,
	}

	cmd.Flags().IntVarP(&profileActivityLimit, "limit", "l", 10, "Maximum number of activity items")
	cmd.Flags().StringVar(&profileActivityCategory, "category", string(api.RecentActivityCategoryAll), "Activity category: all, images, videos, documents, events, reactions")

	return cmd
}

func runProfileActivity(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	username := args[0]
	if profileActivityLimit <= 0 {
		return outputError(jsonOutput, api.ErrCodeInvalidInput, "limit must be greater than 0")
	}
	category, err := api.ParseRecentActivityCategory(profileActivityCategory)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	items, err := client.GetRecentActivity(ctx, username, &api.RecentActivityOptions{Limit: profileActivityLimit, Category: category})
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	return outputActivityItems(jsonOutput, items, recentActivityEmptyMessage(category))
}

func recentActivityEmptyMessage(category api.RecentActivityCategory) string {
	if category == api.RecentActivityCategoryAll {
		return "No recent activity found."
	}

	return fmt.Sprintf("No recent %s activity found.", category)
}

// getAuthenticatedClient creates an API client with stored credentials.
func getAuthenticatedClient() (*api.Client, error) {
	store, err := auth.NewStore()
	if err != nil {
		return nil, fmt.Errorf("failed to access credential store: %w", err)
	}

	creds, err := store.Load()
	if err != nil {
		if errors.Is(err, auth.ErrNoCredentials) {
			return nil, fmt.Errorf("not authenticated. Run: lnk auth login")
		}
		return nil, fmt.Errorf("failed to load credentials: %w", err)
	}

	if !creds.IsValid() {
		return nil, fmt.Errorf("credentials expired. Run: lnk auth login")
	}

	client := api.NewClient(api.WithCredentials(creds))
	return client, nil
}

// handleAPIError converts an API error to output.
func handleAPIError(jsonOutput bool, err error) error {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return outputError(jsonOutput, apiErr.Code, apiErr.Message)
	}
	return outputError(jsonOutput, api.ErrCodeServerError, err.Error())
}

// outputProfile outputs a profile in the appropriate format.
func outputProfile(jsonOutput bool, profile *api.Profile) error {
	if jsonOutput {
		return outputJSON(api.Response[*api.Profile]{
			Success: true,
			Data:    profile,
		})
	}

	// Text output.
	fmt.Printf("Name: %s %s\n", profile.FirstName, profile.LastName)
	if profile.Headline != "" {
		fmt.Printf("Headline: %s\n", profile.Headline)
	}
	if profile.Location != "" {
		fmt.Printf("Location: %s\n", profile.Location)
	}
	if profile.ProfileURL != "" {
		fmt.Printf("URL: %s\n", profile.ProfileURL)
	}
	if profile.URN != "" {
		fmt.Printf("URN: %s\n", profile.URN)
	}
	if profile.Summary != "" {
		fmt.Printf("\nSummary:\n%s\n", profile.Summary)
	}

	return nil
}

func outputFeedItems(jsonOutput bool, items []api.FeedItem, emptyMessage string) error {
	if jsonOutput {
		return outputJSON(api.Response[[]api.FeedItem]{
			Success: true,
			Data:    items,
		})
	}

	if len(items) == 0 {
		fmt.Println(emptyMessage)
		return nil
	}

	for i := range items {
		item := &items[i]
		if i > 0 {
			fmt.Println("---")
		}

		if item.Actor != nil && item.Actor.FirstName != "" {
			fmt.Printf("From: %s\n", item.Actor.FirstName)
		}

		if !item.CreatedAt.IsZero() {
			fmt.Printf("Created: %s\n", item.CreatedAt.Format("2006-01-02 15:04:05"))
		}

		if item.Post != nil && item.Post.Text != "" {
			text := item.Post.Text
			if len(text) > 200 {
				text = text[:197] + "..."
			}
			fmt.Printf("Post: %s\n", text)
		}

		fmt.Printf("URN: %s\n", item.URN)
	}

	return nil
}

func outputActivityItems(jsonOutput bool, items []api.ActivityItem, emptyMessage string) error {
	if jsonOutput {
		return outputJSON(api.Response[[]api.ActivityItem]{
			Success: true,
			Data:    items,
		})
	}

	if len(items) == 0 {
		fmt.Println(emptyMessage)
		return nil
	}

	for i := range items {
		item := &items[i]
		if i > 0 {
			fmt.Println("---")
		}

		if item.ActorName != "" {
			fmt.Printf("From: %s\n", item.ActorName)
		}

		if !item.CreatedAt.IsZero() {
			fmt.Printf("Created: %s\n", item.CreatedAt.Format("2006-01-02 15:04:05"))
		}

		if item.Text != "" {
			text := item.Text
			if len(text) > 200 {
				text = text[:197] + "..."
			}
			fmt.Printf("Post: %s\n", text)
		}

		if item.URL != "" {
			fmt.Printf("URL: %s\n", item.URL)
		}

		fmt.Printf("URN: %s\n", item.URN)
	}

	return nil
}
