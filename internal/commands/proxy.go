package commands

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bvgroup-co/lnk/internal/api"
)

const proxyURLEnv = "LNK_PROXY_URL"

var explicitProxyURL string

// RegisterProxyFlag adds the global proxy flag to a root command.
func RegisterProxyFlag(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&explicitProxyURL, "proxy-url", "", "Proxy URL for LinkedIn requests (or LNK_PROXY_URL)")
}

func clientProxyOptions() []api.ClientOption {
	proxyURL := selectedProxyURL(explicitProxyURL, os.Getenv(proxyURLEnv))
	if proxyURL == "" {
		return nil
	}
	return []api.ClientOption{api.WithProxyURL(proxyURL)}
}

func selectedProxyURL(flagValue, envValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	return strings.TrimSpace(envValue)
}
