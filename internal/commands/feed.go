package commands

import (
	"context"

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

	return outputFeedItems(jsonOutput, items, "No feed items found.")
}
