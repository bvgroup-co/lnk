package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bvgroup-co/lnk/internal/api"
)

var messagesLimit int

// NewMessagesCmd creates the messages command group.
func NewMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "messages",
		Aliases: []string{"msg", "dm"},
		Short:   "Manage LinkedIn messages",
		Long:    `Commands for viewing and sending LinkedIn messages.`,
	}

	cmd.AddCommand(newMessagesListCmd())
	cmd.AddCommand(newMessagesGetCmd())
	cmd.AddCommand(newMessagesSendCmd())
	cmd.AddCommand(newMessagesReplyCmd())

	return cmd
}

func newMessagesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List conversations",
		Long: `List your LinkedIn message conversations.

Examples:
  lnk messages list
  lnk messages list --limit 10`,
		RunE: runMessagesList,
	}

	cmd.Flags().IntVarP(&messagesLimit, "limit", "l", 20, "Maximum number of conversations")

	return cmd
}

func runMessagesList(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	opts := &api.MessagingOptions{
		Limit: messagesLimit,
	}

	conversations, err := client.GetConversations(ctx, opts)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[[]api.Conversation]{
			Success: true,
			Data:    conversations,
		})
	}

	// Text output.
	if len(conversations) == 0 {
		fmt.Println("No conversations found.")
		return nil
	}

	fmt.Printf("Found %d conversations:\n\n", len(conversations))
	for i := range conversations {
		conv := &conversations[i]
		// Build participant names.
		var names []string
		for j := range conv.Participants {
			p := &conv.Participants[j]
			name := strings.TrimSpace(p.FirstName + " " + p.LastName)
			if name != "" {
				names = append(names, name)
			}
		}
		participantStr := strings.Join(names, ", ")
		if participantStr == "" {
			participantStr = "(Unknown)"
		}

		unreadMarker := ""
		if conv.Unread {
			unreadMarker = " [UNREAD]"
		}

		fmt.Printf("%d. %s%s\n", i+1, participantStr, unreadMarker)
		if !conv.LastActivityAt.IsZero() {
			fmt.Printf("   Last activity: %s\n", formatTime(conv.LastActivityAt))
		}
		if conv.TotalEvents > 0 {
			fmt.Printf("   Messages: %d\n", conv.TotalEvents)
		}
		fmt.Printf("   URN: %s\n", conv.URN)
		fmt.Println()
	}

	return nil
}

func newMessagesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <conversation-urn>",
		Short: "Get messages in a conversation",
		Long: `View messages in a specific conversation.

Example:
  lnk messages get "urn:li:fs_conversation:123456"`,
		Args: cobra.ExactArgs(1),
		RunE: runMessagesGet,
	}
}

func runMessagesGet(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	conversationURN := args[0]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	conv, messages, err := client.GetConversation(ctx, conversationURN)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[struct {
			Conversation *api.Conversation `json:"conversation"`
			Messages     []api.Message     `json:"messages"`
		}]{
			Success: true,
			Data: struct {
				Conversation *api.Conversation `json:"conversation"`
				Messages     []api.Message     `json:"messages"`
			}{
				Conversation: conv,
				Messages:     messages,
			},
		})
	}

	// Text output.
	if len(messages) == 0 {
		fmt.Println("No messages in this conversation.")
		return nil
	}

	fmt.Printf("Conversation: %s\n", conv.URN)
	fmt.Printf("Messages (%d):\n\n", len(messages))

	for _, msg := range messages {
		sender := msg.SenderName
		if sender == "" {
			sender = "Unknown"
		}
		timeStr := ""
		if !msg.CreatedAt.IsZero() {
			timeStr = fmt.Sprintf(" (%s)", formatTime(msg.CreatedAt))
		}
		fmt.Printf("[%s]%s:\n", sender, timeStr)
		fmt.Printf("  %s\n\n", msg.Text)
	}

	return nil
}

func newMessagesSendCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "send <profile-urn-or-username> <message>",
		Short: "Send a message to a profile",
		Long: `Send a new message to a LinkedIn profile.

Examples:
  lnk messages send "urn:li:member:123456" "Hello!"
  lnk messages send johndoe "Hi John, wanted to connect!"`,
		Args: cobra.ExactArgs(2),
		RunE: runMessagesSend,
	}
}

func runMessagesSend(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	target := args[0]
	text := args[1]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	// If target doesn't look like a URN, treat it as a username and look up the profile.
	profileURN := target
	if !strings.HasPrefix(target, "urn:") {
		profile, lookupErr := client.GetProfile(ctx, target)
		if lookupErr != nil {
			return handleAPIError(jsonOutput, lookupErr)
		}
		if profile.URN == "" {
			return outputError(jsonOutput, api.ErrCodeNotFound, "could not find profile URN for "+target)
		}
		profileURN = profile.URN
	}

	msg, err := client.SendMessage(ctx, profileURN, text)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[*api.Message]{
			Success: true,
			Data:    msg,
		})
	}

	fmt.Println("Message sent successfully!")
	return nil
}

func newMessagesReplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reply <conversation-urn> <message>",
		Short: "Reply to a conversation",
		Long: `Reply to an existing conversation.

Example:
  lnk messages reply "urn:li:fs_conversation:123456" "Thanks for getting back to me!"`,
		Args: cobra.ExactArgs(2),
		RunE: runMessagesReply,
	}
}

func runMessagesReply(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	conversationURN := args[0]
	text := args[1]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	msg, err := client.SendMessageToConversation(ctx, conversationURN, text)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[*api.Message]{
			Success: true,
			Data:    msg,
		})
	}

	fmt.Println("Reply sent successfully!")
	return nil
}

// formatTime formats a time for display.
func formatTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 2, 2006")
	}
}
