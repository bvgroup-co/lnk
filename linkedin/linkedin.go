// Package linkedin exposes a reusable LinkedIn API client.
package linkedin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/bvgroup-co/lnk/internal/api"
)

const (
	// BaseURL is the default LinkedIn Voyager API base URL.
	BaseURL = api.BaseURL
	// DefaultTimeout is the default HTTP client timeout.
	DefaultTimeout = api.DefaultTimeout
	// DefaultAuthenticatedRequestDelay is the default pacing delay for authenticated requests.
	DefaultAuthenticatedRequestDelay = api.DefaultAuthenticatedRequestDelay

	// ErrCodeAuthExpired indicates LinkedIn rejected the stored session.
	ErrCodeAuthExpired = api.ErrCodeAuthExpired
	// ErrCodeAuthRequired indicates credentials were required but not provided.
	ErrCodeAuthRequired = api.ErrCodeAuthRequired
	// ErrCodeRateLimited indicates LinkedIn returned a rate limit response.
	ErrCodeRateLimited = api.ErrCodeRateLimited
	// ErrCodeNotFound indicates the requested resource was not found.
	ErrCodeNotFound = api.ErrCodeNotFound
	// ErrCodeForbidden indicates LinkedIn denied access to the resource.
	ErrCodeForbidden = api.ErrCodeForbidden
	// ErrCodeServerError indicates LinkedIn returned an unexpected server response.
	ErrCodeServerError = api.ErrCodeServerError
	// ErrCodeNetworkError indicates the request failed before receiving a LinkedIn response.
	ErrCodeNetworkError = api.ErrCodeNetworkError
	// ErrCodeInvalidInput indicates caller-provided input was invalid.
	ErrCodeInvalidInput = api.ErrCodeInvalidInput
	// ErrCodeUnsupported indicates the requested operation is not currently supported.
	ErrCodeUnsupported = api.ErrCodeUnsupported
)

// Error represents a LinkedIn API error with a stable machine-readable code.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Credentials holds LinkedIn authentication cookies.
type Credentials = api.Credentials

// Profile represents a LinkedIn user profile.
type Profile = api.Profile

// ActivityItem represents a recent LinkedIn profile activity item.
type ActivityItem = api.ActivityItem

// RecentActivityCategory identifies a LinkedIn recent activity category.
type RecentActivityCategory = api.RecentActivityCategory

const (
	// RecentActivityCategoryAll fetches the generic profile activity feed.
	RecentActivityCategoryAll = api.RecentActivityCategoryAll
	// RecentActivityCategoryPosts fetches profile post activity.
	RecentActivityCategoryPosts = api.RecentActivityCategoryPosts
	// RecentActivityCategoryImages identifies image activity.
	RecentActivityCategoryImages = api.RecentActivityCategoryImages
	// RecentActivityCategoryVideos identifies video activity.
	RecentActivityCategoryVideos = api.RecentActivityCategoryVideos
	// RecentActivityCategoryDocuments identifies document activity.
	RecentActivityCategoryDocuments = api.RecentActivityCategoryDocuments
	// RecentActivityCategoryEvents identifies event activity.
	RecentActivityCategoryEvents = api.RecentActivityCategoryEvents
	// RecentActivityCategoryReactions fetches profile reaction activity.
	RecentActivityCategoryReactions = api.RecentActivityCategoryReactions
	// RecentActivityCategoryComments fetches profile comment activity.
	RecentActivityCategoryComments = api.RecentActivityCategoryComments
)

// RecentActivityOptions configures recent profile activity fetching.
type RecentActivityOptions = api.RecentActivityOptions

// ClientOption configures a Client.
type ClientOption func(*clientConfig)

type clientConfig struct {
	options []api.ClientOption
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(config *clientConfig) {
		config.options = append(config.options, api.WithHTTPClient(httpClient))
	}
}

// WithBaseURL sets a custom LinkedIn Voyager API base URL.
func WithBaseURL(baseURL string) ClientOption {
	return func(config *clientConfig) {
		config.options = append(config.options, api.WithBaseURL(baseURL))
	}
}

// WithCredentials sets the authentication credentials.
func WithCredentials(credentials *Credentials) ClientOption {
	return func(config *clientConfig) {
		config.options = append(config.options, api.WithCredentials(credentials))
	}
}

// WithAuthenticatedRequestDelay sets the pacing delay for authenticated requests.
func WithAuthenticatedRequestDelay(delay time.Duration) ClientOption {
	return func(config *clientConfig) {
		config.options = append(config.options, api.WithAuthenticatedRequestDelay(delay))
	}
}

// Client is a reusable LinkedIn API client.
type Client struct {
	client *api.Client
}

// NewClient creates a new LinkedIn API client.
func NewClient(options ...ClientOption) *Client {
	config := &clientConfig{}
	for _, option := range options {
		option(config)
	}

	return &Client{client: api.NewClient(config.options...)}
}

// SetCredentials updates the client's credentials.
func (c *Client) SetCredentials(credentials *Credentials) {
	c.client.SetCredentials(credentials)
}

// HasCredentials returns true when credentials are set and unexpired.
func (c *Client) HasCredentials() bool {
	return c.client.HasCredentials()
}

// TestAuth verifies that the current credentials can access the authenticated LinkedIn account.
func (c *Client) TestAuth(ctx context.Context) (*Profile, error) {
	profile, err := c.client.GetMyProfile(ctx)
	return profile, wrapError(err)
}

// GetMyProfile fetches the authenticated user's profile.
func (c *Client) GetMyProfile(ctx context.Context) (*Profile, error) {
	profile, err := c.client.GetMyProfile(ctx)
	return profile, wrapError(err)
}

// GetProfile fetches a profile by public identifier.
func (c *Client) GetProfile(ctx context.Context, publicID string) (*Profile, error) {
	profile, err := c.client.GetProfile(ctx, publicID)
	return profile, wrapError(err)
}

// GetProfileByURN fetches a profile by URN.
func (c *Client) GetProfileByURN(ctx context.Context, urn string) (*Profile, error) {
	profile, err := c.client.GetProfileByURN(ctx, urn)
	return profile, wrapError(err)
}

// GetRecentActivity fetches recent activity for a profile by public identifier.
func (c *Client) GetRecentActivity(ctx context.Context, publicID string, options *RecentActivityOptions) ([]ActivityItem, error) {
	activity, err := c.client.GetRecentActivity(ctx, publicID, options)
	return activity, wrapError(err)
}

// GetProfilePosts fetches recent posts for a profile by public identifier.
func (c *Client) GetProfilePosts(ctx context.Context, publicID string, limit int) ([]ActivityItem, error) {
	activity, err := c.client.GetRecentActivity(ctx, publicID, &api.RecentActivityOptions{
		Limit:    limit,
		Category: api.RecentActivityCategoryPosts,
	})
	return activity, wrapError(err)
}

// ParseRecentActivityCategory validates a recent activity category string.
func ParseRecentActivityCategory(category string) (RecentActivityCategory, error) {
	parsed, err := api.ParseRecentActivityCategory(category)
	return parsed, wrapError(err)
}

// AsError returns a stable LinkedIn API error when err contains one.
func AsError(err error) (*Error, bool) {
	var publicErr *Error
	if errors.As(err, &publicErr) {
		return publicErr, true
	}

	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return convertError(apiErr), true
	}
	return nil, false
}

func wrapError(err error) error {
	if err == nil {
		return nil
	}

	if publicErr, ok := AsError(err); ok {
		return publicErr
	}
	return &Error{Code: ErrCodeServerError, Message: err.Error()}
}

func convertError(err *api.Error) *Error {
	return &Error{Code: err.Code, Message: publicErrorMessage(err)}
}

func publicErrorMessage(err *api.Error) string {
	message := strings.ReplaceAll(err.Message, ". Run: lnk auth login", "")
	message = strings.ReplaceAll(message, " Run: lnk auth login", "")
	return strings.ReplaceAll(message, "lnk auth login", "authenticate with LinkedIn")
}
