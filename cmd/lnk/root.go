// Package main provides the lnk CLI entry point.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/bvgroup-co/lnk/internal/commands"
	"github.com/bvgroup-co/lnk/internal/version"
)

// Global flags.
var jsonOutput bool

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "lnk",
	Short: "A fast LinkedIn CLI for posting, reading, and messaging",
	Long: `lnk is a command-line interface for LinkedIn that allows you to
post updates, read your feed, view profiles, search, and send messages.

Designed to work seamlessly with AI agents through structured JSON output.

Example usage:
  lnk auth login --browser safari    # macOS
  lnk auth login --browser chrome    # macOS/Linux
  lnk profile me --json
  lnk feed --limit 10
  lnk post create "Hello LinkedIn!"`,
	Version: version.Info(),
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Global flags available to all commands
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format (agent-friendly)")
	commands.RegisterProxyFlag(rootCmd)

	// Disable default completion command
	rootCmd.CompletionOptions.DisableDefaultCmd = true

	// Add commands
	rootCmd.AddCommand(commands.NewAuthCmd())
	rootCmd.AddCommand(commands.NewProfileCmd())
	rootCmd.AddCommand(commands.NewFeedCmd())
	rootCmd.AddCommand(commands.NewPostCmd())
	rootCmd.AddCommand(commands.NewSearchCmd())
	rootCmd.AddCommand(commands.NewMessagesCmd())
}
