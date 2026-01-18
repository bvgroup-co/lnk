package commands

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/pp/lnk/internal/api"
)

var searchLimit int

// NewSearchCmd creates the search command group.
func NewSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search LinkedIn",
		Long:  `Search for people, companies, and jobs on LinkedIn.`,
	}

	cmd.AddCommand(newSearchPeopleCmd())
	cmd.AddCommand(newSearchCompaniesCmd())

	return cmd
}

func newSearchPeopleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "people <query>",
		Short: "Search for people",
		Long: `Search for people on LinkedIn.

Examples:
  lnk search people "software engineer"
  lnk search people "product manager" --limit 20`,
		Args: cobra.ExactArgs(1),
		RunE: runSearchPeople,
	}

	cmd.Flags().IntVarP(&searchLimit, "limit", "l", 10, "Maximum number of results")

	return cmd
}

func runSearchPeople(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	query := args[0]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	opts := &api.SearchOptions{
		Limit: searchLimit,
	}

	profiles, err := client.SearchPeople(ctx, query, opts)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[[]api.Profile]{
			Success: true,
			Data:    profiles,
		})
	}

	// Text output.
	if len(profiles) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	fmt.Printf("Found %d people:\n\n", len(profiles))
	for i := range profiles {
		p := &profiles[i]
		fmt.Printf("%d. %s %s\n", i+1, p.FirstName, p.LastName)
		if p.Headline != "" {
			fmt.Printf("   %s\n", p.Headline)
		}
		if p.Location != "" {
			fmt.Printf("   📍 %s\n", p.Location)
		}
		if p.ProfileURL != "" {
			fmt.Printf("   🔗 %s\n", p.ProfileURL)
		}
		fmt.Println()
	}

	return nil
}

func newSearchCompaniesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "companies <query>",
		Short: "Search for companies",
		Long: `Search for companies on LinkedIn.

Examples:
  lnk search companies "anthropic"
  lnk search companies "artificial intelligence" --limit 20`,
		Args: cobra.ExactArgs(1),
		RunE: runSearchCompanies,
	}

	cmd.Flags().IntVarP(&searchLimit, "limit", "l", 10, "Maximum number of results")

	return cmd
}

func runSearchCompanies(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	ctx := context.Background()

	query := args[0]

	client, err := getAuthenticatedClient()
	if err != nil {
		return outputError(jsonOutput, api.ErrCodeAuthRequired, err.Error())
	}

	opts := &api.SearchOptions{
		Limit: searchLimit,
	}

	companies, err := client.SearchCompanies(ctx, query, opts)
	if err != nil {
		return handleAPIError(jsonOutput, err)
	}

	if jsonOutput {
		return outputJSON(api.Response[[]api.Company]{
			Success: true,
			Data:    companies,
		})
	}

	// Text output.
	if len(companies) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	fmt.Printf("Found %d companies:\n\n", len(companies))
	for i := range companies {
		c := &companies[i]
		fmt.Printf("%d. %s\n", i+1, c.Name)
		if c.Industry != "" {
			fmt.Printf("   Industry: %s\n", c.Industry)
		}
		if c.Location != "" {
			fmt.Printf("   📍 %s\n", c.Location)
		}
		if c.FollowerCount != "" {
			fmt.Printf("   👥 %s\n", c.FollowerCount)
		}
		if c.Description != "" {
			desc := c.Description
			if len(desc) > 100 {
				desc = desc[:100] + "..."
			}
			fmt.Printf("   %s\n", desc)
		}
		if c.CompanyURL != "" {
			fmt.Printf("   🔗 %s\n", c.CompanyURL)
		}
		fmt.Println()
	}

	return nil
}
