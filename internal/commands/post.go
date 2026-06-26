package commands

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bvgroup-co/lnk/internal/api"
)

var postFile string

// NewPostCmd creates the post command group.
func NewPostCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "post",
		Short: "Create and manage posts",
		Long:  `Commands for creating and managing LinkedIn posts.`,
	}

	cmd.AddCommand(newPostCreateCmd())
	cmd.AddCommand(newPostGetCmd())
	cmd.AddCommand(newPostDeleteCmd())

	return cmd
}

func newPostCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [text]",
		Short: "Create a new post",
		Long: `Create a new LinkedIn post.

Examples:
  lnk post create "Hello LinkedIn!"
  lnk post create --file post.txt`,
		Args: cobra.MaximumNArgs(1),
		RunE: runPostCreate,
	}

	cmd.Flags().StringVarP(&postFile, "file", "f", "", "Read post content from file")

	return cmd
}

func runPostCreate(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	// Get post text.
	var text string
	switch {
	case postFile != "":
		content, err := os.ReadFile(postFile)
		if err != nil {
			return outputError(jsonOutput, api.ErrCodeInvalidInput, fmt.Sprintf("failed to read file: %v", err))
		}
		text = strings.TrimSpace(string(content))
	case len(args) > 0:
		text = args[0]
	default:
		return outputError(jsonOutput, api.ErrCodeInvalidInput, "provide post text or --file")
	}

	if text == "" {
		return outputError(jsonOutput, api.ErrCodeInvalidInput, "post text cannot be empty")
	}

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	post, err := client.CreatePost(ctx, text)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[*api.Post]{
			Success: true,
			Data:    post,
		})
	}

	fmt.Println("Post created successfully!")
	if post.URN != "" {
		fmt.Printf("URN: %s\n", post.URN)
	}

	return nil
}

func newPostGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <urn>",
		Short: "Get a post by URN",
		Long: `Fetch and display a LinkedIn post by its URN.

Example:
  lnk post get "urn:li:activity:123456789"`,
		Args: cobra.ExactArgs(1),
		RunE: runPostGet,
	}
}

func runPostGet(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	urn := args[0]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	post, err := client.GetPost(ctx, urn)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[*api.Post]{
			Success: true,
			Data:    post,
		})
	}

	// Text output.
	if post.AuthorName != "" {
		fmt.Printf("Author: %s\n", post.AuthorName)
	}
	if post.Text != "" {
		fmt.Printf("Text: %s\n", post.Text)
	}
	if post.URN != "" {
		fmt.Printf("URN: %s\n", post.URN)
	}
	if post.LikeCount > 0 || post.CommentCount > 0 {
		fmt.Printf("Likes: %d, Comments: %d\n", post.LikeCount, post.CommentCount)
	}

	return nil
}

func newPostDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <urn>",
		Short: "Delete a post by URN",
		Long: `Delete a LinkedIn post by its URN.

Example:
  lnk post delete "urn:li:share:123456789"`,
		Args: cobra.ExactArgs(1),
		RunE: runPostDelete,
	}
}

func runPostDelete(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	urn := args[0]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	if err := client.DeletePost(ctx, urn); err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(map[string]any{
			"success": true,
			"message": "Post deleted successfully",
		})
	}

	fmt.Println("Post deleted successfully.")
	return nil
}
