package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pp/lnk/internal/api"
)

var feedLimit int

// NewFeedCmd creates the feed command.
func NewFeedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "feed",
		Short: "Read your LinkedIn feed",
		Long: `Fetch and display your LinkedIn feed.

Examples:
  lnk feed
  lnk feed --limit 20
  lnk feed --json`,
		RunE: runFeed,
	}

	cmd.Flags().IntVarP(&feedLimit, "limit", "l", 10, "Number of feed items to fetch")

	return cmd
}

func runFeed(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	items, err := client.GetFeed(ctx, &api.FeedOptions{Limit: feedLimit})
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[[]api.FeedItem]{
			Success: true,
			Data:    items,
		})
	}

	// Text output.
	if len(items) == 0 {
		fmt.Println("No feed items found.")
		return nil
	}

	for i, item := range items {
		if i > 0 {
			fmt.Println("---")
		}

		if item.Actor != nil && item.Actor.FirstName != "" {
			fmt.Printf("From: %s\n", item.Actor.FirstName)
		}

		if item.Post != nil && item.Post.Text != "" {
			// Truncate long posts in text mode.
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
