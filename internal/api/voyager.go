package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	defaultActivityLimit   = 10
	maxCategoryRawFetchCap = 100
	maxGraphQLUpdatesPages = 10
)

const (
	defaultProfilePostsQueryID     = "voyagerFeedDashProfileUpdates.4af00b28d60ed0f1488018948daad822"
	defaultProfileCommentsQueryID  = "voyagerFeedDashProfileUpdates.8f05a4e5ad12d9cb2b56eaa22afbcab9"
	defaultProfileReactionsQueryID = "voyagerFeedDashProfileUpdates.3a42619bc23360ce8c29e737277e2ea9"
)

const allowedRecentActivityCategories = "all, posts, images, videos, documents, events, reactions, comments"

const commentMessageKey = "message"

const reactionKey = "reaction"

const jsonURNKey = "urn"

const maxFilteredActivityPages = 3

const unsupportedRecentActivityCategoryMessage = "LinkedIn Web UI matching for category %q is not currently implemented. The previous implementation used local heuristics and may return incorrect results. Capture the Web UI request shape or retry with --experimental-local-filter if you explicitly want the legacy heuristic behavior."

var (
	activityURNPattern = regexp.MustCompile(`urn:li:activity:\d+`)
	reactionURNPattern = regexp.MustCompile(`^urn:li:reaction:\(([^,]+),(urn:li:activity:\d+)\)$`)
	commentURNPattern  = regexp.MustCompile(`urn:li:comment:\(((?:urn:li:)?(?:activity|ugcPost):\d+),([^,)]+)\)`)
	fsdCommentPattern  = regexp.MustCompile(`urn:li:fsd_comment:\((\d+),((?:urn:li:)?(?:activity|ugcPost):\d+)\)`)
)

type recentActivityEndpoint struct {
	path     string
	query    url.Values
	rawQuery string
	headers  map[string]string
}

type graphQLProfileUpdatesCategory struct {
	Category       RecentActivityCategory
	QueryID        string
	CollectionName string
}

// VoyagerResponse wraps LinkedIn's Voyager API response format.
type VoyagerResponse struct {
	Data     json.RawMessage   `json:"data"`
	Included []json.RawMessage `json:"included"`
	Paging   *Paging           `json:"paging,omitempty"`
}

// Paging contains pagination information.
type Paging struct {
	Count int    `json:"count"`
	Start int    `json:"start"`
	Total int    `json:"total,omitempty"`
	Links []Link `json:"links,omitempty"`
}

// Link represents a pagination link.
type Link struct {
	Rel  string `json:"rel"`
	Href string `json:"href"`
	Type string `json:"type"`
}

// ProfileResponse represents the profile API response.
type ProfileResponse struct {
	Profile     *Profile `json:"profile"`
	RawData     json.RawMessage
	RawIncluded []json.RawMessage
}

// GetMyProfile fetches the authenticated user's profile.
func (c *Client) GetMyProfile(ctx context.Context) (*Profile, error) {
	// Use the /me endpoint to get current user.
	var result VoyagerResponse
	err := c.Get(ctx, "/identity/dash/profiles?q=memberIdentity&memberIdentity=me&decorationId=com.linkedin.voyager.dash.deco.identity.profile.WebTopCardCore-19", nil, &result)
	if err != nil {
		return nil, err
	}

	return parseProfileFromResponse(&result)
}

// GetProfile fetches a profile by public identifier (username).
func (c *Client) GetProfile(ctx context.Context, publicID string) (*Profile, error) {
	// Use the voyagerIdentityDashProfiles endpoint.
	query := url.Values{}
	query.Set("q", "memberIdentity")
	query.Set("memberIdentity", publicID)
	query.Set("decorationId", "com.linkedin.voyager.dash.deco.identity.profile.WebTopCardCore-19")

	var result VoyagerResponse
	if err := c.Get(ctx, "/voyagerIdentityDashProfiles", query, &result); err != nil {
		return nil, err
	}

	return parseProfileFromResponse(&result)
}

// GetProfileByURN fetches a profile by URN.
func (c *Client) GetProfileByURN(ctx context.Context, urn string) (*Profile, error) {
	// Extract the member ID from URN.
	// URN format: urn:li:member:123456 or urn:li:fsd_profile:ACoAAAxxxxxx
	parts := strings.Split(urn, ":")
	if len(parts) < 4 {
		return nil, &Error{
			Code:    ErrCodeInvalidInput,
			Message: fmt.Sprintf("invalid URN format: %s", urn),
		}
	}

	memberID := parts[len(parts)-1]

	// Use the profile API with URN.
	query := url.Values{}
	query.Set("q", "memberIdentity")
	query.Set("memberIdentity", memberID)
	query.Set("decorationId", "com.linkedin.voyager.dash.deco.identity.profile.WebTopCardCore-19")

	var result VoyagerResponse
	if err := c.Get(ctx, "/identity/dash/profiles", query, &result); err != nil {
		return nil, err
	}

	return parseProfileFromResponse(&result)
}

// parseProfileFromResponse extracts a Profile from a Voyager response.
func parseProfileFromResponse(resp *VoyagerResponse) (*Profile, error) {
	if resp == nil {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "empty response",
		}
	}

	// First, try to get the target URN from data.*elements.
	targetURN := ""
	if len(resp.Data) > 0 {
		var dataResp struct {
			Elements []string `json:"*elements"`
		}
		if err := json.Unmarshal(resp.Data, &dataResp); err == nil && len(dataResp.Elements) > 0 {
			targetURN = dataResp.Elements[0]
		}
	}

	// Look for the profile with matching URN in included array.
	for _, item := range resp.Included {
		var entity map[string]json.RawMessage
		if err := json.Unmarshal(item, &entity); err != nil {
			continue
		}

		// Check for profile entity.
		if entityURN, ok := entity["entityUrn"]; ok {
			var urn string
			if err := json.Unmarshal(entityURN, &urn); err == nil {
				// If we have a target URN, only match that one.
				if targetURN != "" {
					if urn == targetURN {
						profile := &Profile{}
						if err := parseProfileEntity(item, profile); err == nil {
							return profile, nil
						}
					}
					continue
				}

				// Otherwise, return first profile found.
				if strings.Contains(urn, "fsd_profile") || strings.Contains(urn, "member") {
					profile := &Profile{}
					if err := parseProfileEntity(item, profile); err == nil {
						if profile.FirstName != "" || profile.PublicID != "" {
							return profile, nil
						}
					}
				}
			}
		}
	}

	// Try parsing the data field directly.
	if len(resp.Data) > 0 {
		profile := &Profile{}
		if err := parseProfileEntity(resp.Data, profile); err == nil {
			if profile.FirstName != "" || profile.PublicID != "" {
				return profile, nil
			}
		}
	}

	return nil, &Error{
		Code:    ErrCodeNotFound,
		Message: "profile not found in response",
	}
}

// parseProfileEntity extracts profile fields from a JSON entity.
func parseProfileEntity(data json.RawMessage, profile *Profile) error {
	var entity struct {
		EntityURN        string `json:"entityUrn"`
		PublicIdentifier string `json:"publicIdentifier"`
		FirstName        string `json:"firstName"`
		LastName         string `json:"lastName"`
		Headline         string `json:"headline"`
		Summary          string `json:"summary"`
		LocationName     string `json:"locationName"`
		GeoLocationName  string `json:"geoLocationName"`
		ProfilePicture   struct {
			DisplayImageReference struct {
				VectorImage struct {
					RootURL string `json:"rootUrl"`
				} `json:"vectorImage"`
			} `json:"displayImageReference"`
		} `json:"profilePicture"`
		// Alternative field names.
		Occupation  string `json:"occupation"`
		MiniProfile struct {
			FirstName        string `json:"firstName"`
			LastName         string `json:"lastName"`
			Occupation       string `json:"occupation"`
			PublicIdentifier string `json:"publicIdentifier"`
			EntityUrn        string `json:"entityUrn"`
		} `json:"miniProfile"`
	}

	if err := json.Unmarshal(data, &entity); err != nil {
		return err
	}

	// Set fields from direct properties.
	if entity.EntityURN != "" {
		profile.URN = entity.EntityURN
	}
	if entity.PublicIdentifier != "" {
		profile.PublicID = entity.PublicIdentifier
		profile.ProfileURL = fmt.Sprintf("https://www.linkedin.com/in/%s", entity.PublicIdentifier)
	}
	if entity.FirstName != "" {
		profile.FirstName = entity.FirstName
	}
	if entity.LastName != "" {
		profile.LastName = entity.LastName
	}
	if entity.Headline != "" {
		profile.Headline = entity.Headline
	} else if entity.Occupation != "" {
		profile.Headline = entity.Occupation
	}
	if entity.Summary != "" {
		profile.Summary = entity.Summary
	}
	if entity.LocationName != "" {
		profile.Location = entity.LocationName
	} else if entity.GeoLocationName != "" {
		profile.Location = entity.GeoLocationName
	}
	if entity.ProfilePicture.DisplayImageReference.VectorImage.RootURL != "" {
		profile.ProfilePicURL = entity.ProfilePicture.DisplayImageReference.VectorImage.RootURL
	}

	// Set fields from miniProfile if main fields are empty.
	if profile.FirstName == "" && entity.MiniProfile.FirstName != "" {
		profile.FirstName = entity.MiniProfile.FirstName
	}
	if profile.LastName == "" && entity.MiniProfile.LastName != "" {
		profile.LastName = entity.MiniProfile.LastName
	}
	if profile.Headline == "" && entity.MiniProfile.Occupation != "" {
		profile.Headline = entity.MiniProfile.Occupation
	}
	if profile.PublicID == "" && entity.MiniProfile.PublicIdentifier != "" {
		profile.PublicID = entity.MiniProfile.PublicIdentifier
		profile.ProfileURL = fmt.Sprintf("https://www.linkedin.com/in/%s", entity.MiniProfile.PublicIdentifier)
	}
	if profile.URN == "" && entity.MiniProfile.EntityUrn != "" {
		profile.URN = entity.MiniProfile.EntityUrn
	}

	return nil
}

// FeedOptions configures feed fetching.
type FeedOptions struct {
	Limit int
	Start int
}

func normalizeFeedOptions(opts *FeedOptions, defaultLimit int) FeedOptions {
	if opts == nil {
		return FeedOptions{Limit: defaultLimit}
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}
	return *opts
}

func normalizeRecentActivityOptions(opts *RecentActivityOptions) RecentActivityOptions {
	if opts == nil {
		return RecentActivityOptions{Limit: defaultActivityLimit, Category: RecentActivityCategoryAll}
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultActivityLimit
	}
	if opts.Category == "" {
		opts.Category = RecentActivityCategoryAll
	}
	return *opts
}

// ParseRecentActivityCategory validates a user-provided recent activity category.
func ParseRecentActivityCategory(category string) (RecentActivityCategory, error) {
	switch RecentActivityCategory(category) {
	case RecentActivityCategoryAll,
		RecentActivityCategoryPosts,
		RecentActivityCategoryImages,
		RecentActivityCategoryVideos,
		RecentActivityCategoryDocuments,
		RecentActivityCategoryEvents,
		RecentActivityCategoryReactions,
		RecentActivityCategoryComments:
		return RecentActivityCategory(category), nil
	default:
		return "", &Error{
			Code:    ErrCodeInvalidInput,
			Message: fmt.Sprintf("invalid category %q; allowed values: %s", category, allowedRecentActivityCategories),
		}
	}
}

func validateLinkedInUsername(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", &Error{
			Code:    ErrCodeInvalidInput,
			Message: "username is required",
		}
	}

	if strings.Contains(username, "/") || strings.Contains(username, "?") || strings.Contains(username, "#") {
		return "", &Error{
			Code:    ErrCodeInvalidInput,
			Message: fmt.Sprintf("invalid username: %s", username),
		}
	}

	return username, nil
}

// GetRecentActivity fetches recent activity for a profile by public identifier.
func (c *Client) GetRecentActivity(ctx context.Context, username string, opts *RecentActivityOptions) ([]ActivityItem, error) {
	username, err := validateLinkedInUsername(username)
	if err != nil {
		return nil, err
	}

	activityOptions := normalizeRecentActivityOptions(opts)
	if _, parseErr := ParseRecentActivityCategory(string(activityOptions.Category)); parseErr != nil {
		return nil, parseErr
	}
	if isUnsupportedDefaultRecentActivityCategory(activityOptions.Category, activityOptions.ExperimentalLocalFilter) {
		return nil, unsupportedRecentActivityCategoryError(activityOptions.Category)
	}

	profile, err := c.GetProfile(ctx, username)
	if err != nil {
		return nil, err
	}
	if profile.URN == "" {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "profile response did not include a profile URN",
		}
	}
	if graphQLCategory, ok := c.graphQLProfileUpdatesCategory(activityOptions.Category); ok && !activityOptions.ExperimentalLocalFilter {
		return c.getGraphQLProfileUpdates(ctx, username, profile.URN, activityOptions, graphQLCategory)
	}

	endpoints := buildRecentActivityEndpoints(username, profile.URN, activityOptions)
	var lastErr error
	for _, endpoint := range endpoints {
		items, err := c.getFilteredRecentActivityEndpoint(ctx, endpoint, activityOptions)
		if err != nil {
			if isTerminalActivityError(err) {
				return nil, err
			}
			lastErr = err
			continue
		}

		if len(items) > activityOptions.Limit {
			items = items[:activityOptions.Limit]
		}

		return items, nil
	}

	if lastErr != nil {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "LinkedIn recent activity API is currently unavailable or returned an unsupported response shape",
		}
	}

	return []ActivityItem{}, nil
}

// GetRecentActivityDebugShape fetches safe structural metadata for a recent activity response.
func (c *Client) GetRecentActivityDebugShape(ctx context.Context, username string, opts *RecentActivityOptions) (*ActivityDebugShape, error) {
	username, err := validateLinkedInUsername(username)
	if err != nil {
		return nil, err
	}

	activityOptions := normalizeRecentActivityOptions(opts)
	if _, parseErr := ParseRecentActivityCategory(string(activityOptions.Category)); parseErr != nil {
		return nil, parseErr
	}

	profile, err := c.GetProfile(ctx, username)
	if err != nil {
		return nil, err
	}
	if profile.URN == "" {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "profile response did not include a profile URN",
		}
	}
	if graphQLCategory, ok := c.graphQLProfileUpdatesCategory(activityOptions.Category); ok && !activityOptions.ExperimentalLocalFilter {
		endpoint := c.buildGraphQLProfileUpdatesEndpoint(username, profile.URN, activityOptions, graphQLCategory, "")
		return c.getRecentActivityDebugShapeEndpoint(ctx, endpoint)
	}

	endpoints := buildRecentActivityEndpoints(username, profile.URN, activityOptions)
	if len(endpoints) == 0 {
		return nil, &Error{Code: ErrCodeServerError, Message: "no recent activity endpoints configured"}
	}

	return c.getRecentActivityDebugShapeEndpoint(ctx, endpoints[0])
}

func unsupportedRecentActivityCategoryError(category RecentActivityCategory) *Error {
	return &Error{
		Code:    ErrCodeUnsupported,
		Message: fmt.Sprintf(unsupportedRecentActivityCategoryMessage, category),
	}
}

func isUnsupportedDefaultRecentActivityCategory(category RecentActivityCategory, experimentalLocalFilter bool) bool {
	if experimentalLocalFilter {
		return false
	}

	switch category {
	case RecentActivityCategoryAll,
		RecentActivityCategoryPosts,
		RecentActivityCategoryReactions,
		RecentActivityCategoryComments:
		return false
	case RecentActivityCategoryImages,
		RecentActivityCategoryVideos,
		RecentActivityCategoryDocuments,
		RecentActivityCategoryEvents:
		return true
	default:
		return false
	}
}

func (c *Client) getRecentActivityDebugShapeEndpoint(ctx context.Context, endpoint recentActivityEndpoint) (*ActivityDebugShape, error) {
	if err := c.checkConfig(); err != nil {
		return nil, err
	}

	req := &Request{
		Method:      http.MethodGet,
		Path:        endpoint.path,
		Query:       endpoint.query,
		RawQuery:    endpoint.rawQuery,
		Headers:     endpoint.headers,
		RequireAuth: true,
	}
	httpReq, err := c.buildRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	if pacingErr := c.waitForAuthenticatedRequest(ctx, req); pacingErr != nil {
		return nil, pacingErr
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &Error{Code: ErrCodeNetworkError, Message: fmt.Sprintf("network error: %v", c.sanitizeErrorMessage(err.Error()))}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &Error{Code: ErrCodeNetworkError, Message: fmt.Sprintf("failed to read response: %v", err)}
	}

	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		return nil, classifyRedirect(resp.Header.Get("Location"))
	}
	if resp.StatusCode >= 400 {
		return nil, c.handleErrorResponse(resp.StatusCode, body)
	}

	shape, err := buildActivityDebugShape(endpoint, resp.StatusCode, body)
	if err != nil {
		return nil, err
	}
	return shape, nil
}

func (c *Client) getFilteredRecentActivityEndpoint(ctx context.Context, endpoint recentActivityEndpoint, opts RecentActivityOptions) ([]ActivityItem, error) {
	items := make([]ActivityItem, 0, opts.Limit)
	pageCount := recentActivityPageCount(opts.Category)
	pageSize := recentActivityFetchCount(opts)

	for page := 0; page < pageCount; page++ {
		pageEndpoint := endpoint.withStart(opts.Start + page*pageSize)
		var result VoyagerResponse
		if err := c.getRecentActivityEndpoint(ctx, pageEndpoint, &result); err != nil {
			return nil, err
		}

		pageItems, err := parseRecentActivityFromResponse(&result)
		if err != nil {
			return nil, err
		}

		items = append(items, filterRecentActivityByCategory(pageItems, opts.Category)...)
		if len(items) >= opts.Limit || len(pageItems) < pageSize || opts.Category == RecentActivityCategoryAll {
			return items, nil
		}
	}

	return items, nil
}

func (e recentActivityEndpoint) withStart(start int) recentActivityEndpoint {
	query := make(url.Values, len(e.query))
	for key, values := range e.query {
		query[key] = append([]string(nil), values...)
	}
	query.Set("start", fmt.Sprintf("%d", start))

	return recentActivityEndpoint{
		path:     e.path,
		query:    query,
		rawQuery: e.rawQuery,
		headers:  e.headers,
	}
}

func (c *Client) getRecentActivityEndpoint(ctx context.Context, endpoint recentActivityEndpoint, result *VoyagerResponse) error {
	return c.Do(ctx, &Request{
		Method:      http.MethodGet,
		Path:        endpoint.path,
		Query:       endpoint.query,
		RawQuery:    endpoint.rawQuery,
		Headers:     endpoint.headers,
		RequireAuth: true,
	}, result)
}

func defaultRecentActivityGraphQLConfig() RecentActivityGraphQLConfig {
	return RecentActivityGraphQLConfig{
		ProfilePostsQueryID:     defaultProfilePostsQueryID,
		ProfileCommentsQueryID:  defaultProfileCommentsQueryID,
		ProfileReactionsQueryID: defaultProfileReactionsQueryID,
	}
}

func (c *Client) graphQLProfileUpdatesCategory(category RecentActivityCategory) (graphQLProfileUpdatesCategory, bool) {
	config := c.recentActivityGraphQL
	switch category {
	case RecentActivityCategoryPosts:
		return graphQLProfileUpdatesCategory{
			Category:       category,
			QueryID:        firstNonEmpty(config.ProfilePostsQueryID, defaultProfilePostsQueryID),
			CollectionName: "feedDashProfileUpdatesByMemberShareFeed",
		}, true
	case RecentActivityCategoryComments:
		return graphQLProfileUpdatesCategory{
			Category:       category,
			QueryID:        firstNonEmpty(config.ProfileCommentsQueryID, defaultProfileCommentsQueryID),
			CollectionName: "feedDashProfileUpdatesByMemberComments",
		}, true
	case RecentActivityCategoryReactions:
		return graphQLProfileUpdatesCategory{
			Category:       category,
			QueryID:        firstNonEmpty(config.ProfileReactionsQueryID, defaultProfileReactionsQueryID),
			CollectionName: "feedDashProfileUpdatesByMemberReactions",
		}, true
	case RecentActivityCategoryAll,
		RecentActivityCategoryImages,
		RecentActivityCategoryVideos,
		RecentActivityCategoryDocuments,
		RecentActivityCategoryEvents:
		return graphQLProfileUpdatesCategory{}, false
	default:
		return graphQLProfileUpdatesCategory{}, false
	}
}

func (c *Client) getGraphQLProfileUpdates(ctx context.Context, username, profileURN string, opts RecentActivityOptions, category graphQLProfileUpdatesCategory) ([]ActivityItem, error) {
	items := make([]ActivityItem, 0, opts.Limit)
	start := opts.Start
	paginationToken := ""

	for page := 0; page < maxGraphQLUpdatesPages; page++ {
		endpoint := c.buildGraphQLProfileUpdatesEndpoint(username, profileURN, RecentActivityOptions{Limit: opts.Limit, Start: start}, category, paginationToken)
		var result VoyagerResponse
		if err := c.getRecentActivityEndpoint(ctx, endpoint, &result); err != nil {
			return nil, err
		}

		pageItems, nextPaginationToken, err := parseGraphQLProfileUpdatesResponse(&result, category)
		if err != nil {
			return nil, err
		}
		if len(pageItems) == 0 {
			return items, nil
		}

		items = append(items, pageItems...)
		if len(items) >= opts.Limit {
			return items[:opts.Limit], nil
		}
		if nextPaginationToken == "" {
			return items, nil
		}

		paginationToken = nextPaginationToken
		start += opts.Limit
	}

	return items, nil
}

func (c *Client) buildGraphQLProfileUpdatesEndpoint(username, profileURN string, opts RecentActivityOptions, category graphQLProfileUpdatesCategory, paginationToken string) recentActivityEndpoint {
	return recentActivityEndpoint{
		path:     "/graphql",
		rawQuery: graphQLProfileUpdatesRawQuery(opts.Limit, opts.Start, profileURN, category.QueryID, paginationToken),
		headers:  recentActivityHeaders(username, category.Category),
	}
}

func graphQLProfileUpdatesRawQuery(count, start int, profileURN, queryID, paginationToken string) string {
	variables := strings.Builder{}
	variables.WriteString(fmt.Sprintf("(count:%d,start:%d,profileUrn:%s", count, start, escapedGraphQLVariableValue(normalizeGraphQLProfileURN(profileURN))))
	if paginationToken != "" {
		variables.WriteString(",paginationToken:")
		variables.WriteString(escapedGraphQLVariableValue(paginationToken))
	}
	variables.WriteString(")")

	return "includeWebMetadata=true&variables=" + variables.String() + "&queryId=" + queryID
}

func normalizeGraphQLProfileURN(profileURN string) string {
	return strings.Replace(profileURN, "urn:li:fs_profile:", "urn:li:fsd_profile:", 1)
}

func escapedGraphQLVariableValue(value string) string {
	return url.QueryEscape(value)
}

// GetProfileActivity fetches recent activity for a profile by public identifier.
func (c *Client) GetProfileActivity(ctx context.Context, publicID string, opts *FeedOptions) ([]FeedItem, error) {
	feedOptions := normalizeFeedOptions(opts, defaultActivityLimit)
	items, err := c.GetRecentActivity(ctx, publicID, &RecentActivityOptions{
		Limit: feedOptions.Limit,
		Start: feedOptions.Start,
	})
	if err != nil {
		return nil, err
	}

	return activityItemsToFeedItems(items), nil
}

func buildRecentActivityEndpoints(username, profileURN string, opts RecentActivityOptions) []recentActivityEndpoint {
	count := fmt.Sprintf("%d", recentActivityFetchCount(opts))
	start := fmt.Sprintf("%d", opts.Start)

	return []recentActivityEndpoint{
		{
			path: "/feed/updatesV2",
			query: url.Values{
				"q":          {"memberShareFeed"},
				"profileUrn": {profileURN},
				"count":      {count},
				"start":      {start},
			},
			headers: recentActivityHeaders(username, opts.Category),
		},
		{
			path: "/feed/updates",
			query: url.Values{
				"profileId": {username},
				"q":         {"memberShareFeed"},
				"moduleKey": {"member-share"},
				"count":     {count},
				"start":     {start},
			},
			headers: recentActivityHeaders(username, opts.Category),
		},
	}
}

func recentActivityHeaders(username string, category RecentActivityCategory) map[string]string {
	return map[string]string{
		"Referer": recentActivityUIURL(username, category),
	}
}

func recentActivityUIURL(username string, category RecentActivityCategory) string {
	return fmt.Sprintf("https://www.linkedin.com/in/%s/recent-activity/%s/", username, category)
}

func recentActivityFetchCount(opts RecentActivityOptions) int {
	if opts.Category == RecentActivityCategoryAll {
		return opts.Limit
	}

	count := opts.Limit * 5
	if count > maxCategoryRawFetchCap {
		return maxCategoryRawFetchCap
	}
	return count
}

func recentActivityPageCount(category RecentActivityCategory) int {
	if category == RecentActivityCategoryAll {
		return 1
	}

	return maxFilteredActivityPages
}

func isTerminalActivityError(err error) bool {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		return false
	}

	switch apiErr.Code {
	case ErrCodeAuthExpired, ErrCodeAuthRequired, ErrCodeForbidden, ErrCodeRateLimited:
		return true
	default:
		return false
	}
}

func activityItemsToFeedItems(items []ActivityItem) []FeedItem {
	feedItems := make([]FeedItem, 0, len(items))
	for i := range items {
		item := &items[i]
		feedItem := FeedItem{
			URN:       item.URN,
			Type:      item.Type,
			CreatedAt: item.CreatedAt,
		}
		if item.ActorURN != "" || item.ActorName != "" {
			feedItem.Actor = &Profile{
				URN:       item.ActorURN,
				FirstName: item.ActorName,
			}
		}
		if item.Text != "" {
			feedItem.Post = &Post{
				URN:          item.URN,
				AuthorURN:    item.ActorURN,
				AuthorName:   item.ActorName,
				Text:         item.Text,
				CreatedAt:    item.CreatedAt,
				LikeCount:    item.LikeCount,
				CommentCount: item.CommentCount,
				ShareCount:   item.ShareCount,
			}
		}
		feedItems = append(feedItems, feedItem)
	}

	return feedItems
}

// GetFeed fetches the user's LinkedIn feed.
// Note: LinkedIn has restricted their feed API. This may not work reliably.
func (c *Client) GetFeed(ctx context.Context, opts *FeedOptions) ([]FeedItem, error) {
	feedOptions := normalizeFeedOptions(opts, 10)

	// Try multiple endpoint formats as LinkedIn changes them frequently.
	endpoints := []struct {
		path  string
		query url.Values
	}{
		{
			path: "/feed/updatesV2",
			query: url.Values{
				"count":     {fmt.Sprintf("%d", feedOptions.Limit)},
				"start":     {fmt.Sprintf("%d", feedOptions.Start)},
				"q":         {"feedByHasLikedOrCommented"},
				"moduleKey": {"feedModule"},
			},
		},
		{
			path: "/feed/updatesV2",
			query: url.Values{
				"count":    {fmt.Sprintf("%d", feedOptions.Limit)},
				"start":    {fmt.Sprintf("%d", feedOptions.Start)},
				"q":        {"feedByType"},
				"feedType": {"HOMEPAGE"},
			},
		},
	}

	var lastErr error
	for _, ep := range endpoints {
		var result VoyagerResponse
		if err := c.Get(ctx, ep.path, ep.query, &result); err != nil {
			lastErr = err
			continue
		}

		items, err := parseFeedFromResponse(&result)
		if err != nil {
			lastErr = err
			continue
		}

		if len(items) > 0 {
			return items, nil
		}
	}

	if lastErr != nil {
		// Provide helpful error message about LinkedIn API changes.
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "LinkedIn feed API is currently unavailable. LinkedIn frequently changes their internal API. Try 'lnk profile get <username>' to view specific profiles instead.",
		}
	}

	return []FeedItem{}, nil
}

// parseFeedFromResponse extracts feed items from a Voyager response.
func parseFeedFromResponse(resp *VoyagerResponse) ([]FeedItem, error) {
	if resp == nil {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "empty response",
		}
	}

	var items []FeedItem

	// Feed items are typically in the included array.
	for _, raw := range resp.Included {
		var entity map[string]json.RawMessage
		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}

		// Look for update entities.
		if typeField, ok := entity["$type"]; ok {
			var typeName string
			if err := json.Unmarshal(typeField, &typeName); err == nil {
				if strings.Contains(typeName, "Update") || strings.Contains(typeName, "Activity") {
					item, err := parseFeedItem(raw)
					if err == nil && item != nil {
						items = append(items, *item)
					}
				}
			}
		}
	}

	return items, nil
}

// parseRecentActivityFromResponse extracts recent activity items from Voyager responses.
func parseRecentActivityFromResponse(resp *VoyagerResponse) ([]ActivityItem, error) {
	if resp == nil {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "empty response",
		}
	}

	activityEntities := collectActivityEntities(resp)
	items := make([]ActivityItem, 0)
	candidateCount := 0
	parseErrors := make([]string, 0)
	for _, raw := range primaryActivityElements(resp.Data, resp.Included) {
		if !isActivityCandidate(raw) {
			continue
		}

		candidateCount++
		item, err := parseActivityItem(raw, activityEntities)
		if err == nil && item != nil {
			items = append(items, *item)
			continue
		}
		if err != nil {
			parseErrors = append(parseErrors, err.Error())
		}
	}

	if candidateCount > 0 && len(items) == 0 {
		message := "LinkedIn recent activity response contained activity candidates but no supported activity items"
		if len(parseErrors) > 0 {
			message = fmt.Sprintf("%s: %s", message, strings.Join(parseErrors, "; "))
		}

		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: message,
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	return dedupeActivityItems(items), nil
}

func parseGraphQLProfileUpdatesResponse(resp *VoyagerResponse, category graphQLProfileUpdatesCategory) ([]ActivityItem, string, error) {
	if resp == nil {
		return nil, "", &Error{Code: ErrCodeServerError, Message: "empty response"}
	}

	elements, paginationToken, err := graphQLProfileUpdateElements(resp, category.CollectionName)
	if err != nil {
		return nil, "", err
	}

	commentIndex := indexGraphQLCommentEntities(resp.Included)
	items := make([]ActivityItem, 0, len(elements))
	for _, raw := range elements {
		item, parseErr := parseGraphQLProfileUpdate(raw, category.Category)
		if parseErr != nil {
			return nil, "", parseErr
		}
		attachGraphQLCommentDetails(item, raw, commentIndex)
		items = append(items, *item)
	}

	return dedupeActivityItems(items), paginationToken, nil
}

type graphQLCommentEntity struct {
	URN       string
	EntityURN string
	ID        string
	Actor     *ActivityActor
	ActorURN  string
	ActorName string
	Text      string
	CreatedAt time.Time
	SocialURN string
}

type graphQLCommentIndex struct {
	entities   map[string]graphQLCommentEntity
	references map[string][]string
}

func indexGraphQLCommentEntities(included []json.RawMessage) graphQLCommentIndex {
	indexed := graphQLCommentIndex{
		entities:   make(map[string]graphQLCommentEntity),
		references: make(map[string][]string),
	}
	for _, raw := range included {
		comment := parseGraphQLCommentEntity(raw)
		if comment.URN == "" {
			indexGraphQLCommentReferences(raw, indexed.references)
			continue
		}

		indexed.entities[comment.URN] = comment
		if comment.EntityURN != "" {
			indexed.entities[comment.EntityURN] = comment
		}
		if comment.ID != "" {
			indexed.entities[comment.ID] = comment
		}
		indexGraphQLCommentReferences(raw, indexed.references)
	}

	return indexed
}

func indexGraphQLCommentReferences(data json.RawMessage, references map[string][]string) {
	keys := graphQLCommentReferenceKeysFromData(data)
	if len(keys) == 0 {
		return
	}

	for _, urn := range graphQLObjectURNs(data) {
		references[urn] = appendUniqueStrings(references[urn], keys...)
	}
}

func graphQLCommentReferenceKeysFromData(data json.RawMessage) []string {
	var entity struct {
		Comments struct {
			Elements []string `json:"*elements"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(data, &entity); err == nil && len(entity.Comments.Elements) > 0 {
		keys := make([]string, 0, len(entity.Comments.Elements)*2)
		for _, element := range entity.Comments.Elements {
			keys = appendUniqueStrings(keys, commentLookupKeysFromURN(element)...)
		}

		return keys
	}

	return graphQLCommentKeysFromData(data)
}

func graphQLObjectURNs(data json.RawMessage) []string {
	var entity struct {
		EntityURN string `json:"entityUrn"`
		URN       string `json:"urn"`
	}
	if err := json.Unmarshal(data, &entity); err != nil {
		return nil
	}

	return nonEmptyUniqueStrings(entity.EntityURN, entity.URN)
}

func parseGraphQLCommentEntity(data json.RawMessage) graphQLCommentEntity {
	var entity map[string]any
	if err := json.Unmarshal(data, &entity); err != nil {
		return graphQLCommentEntity{}
	}

	entityURN := stringField(entity, "entityUrn")
	canonicalURN := firstNonEmpty(firstCommentURNFromText(stringField(entity, "urn")), canonicalCommentURNFromFSDCommentURN(entityURN), firstCommentURNFromText(entityURN))
	if canonicalURN == "" {
		return graphQLCommentEntity{}
	}

	return graphQLCommentEntity{
		URN:       canonicalURN,
		EntityURN: entityURN,
		ID:        commentIDFromURN(canonicalURN),
		Actor:     activityActorPtrFromObject(entity),
		ActorURN:  actorURNFromObject(entity),
		ActorName: actorNameFromObject(entity),
		Text:      graphQLCommentTextFromObject(entity),
		CreatedAt: graphQLCommentCreatedAtFromObject(entity),
		SocialURN: stringField(entity, "*socialDetail", "socialDetailUrn"),
	}
}

func attachGraphQLCommentDetails(item *ActivityItem, data json.RawMessage, commentIndex graphQLCommentIndex) {
	if item.ContentCategory != RecentActivityCategoryComments || len(commentIndex.entities) == 0 {
		return
	}

	comment, ok := findGraphQLCommentEntity(data, item.RawURN, commentIndex)
	if !ok {
		return
	}

	parentText := item.Text
	item.URN = comment.URN
	item.CommentURN = comment.URN
	item.CommentText = comment.Text
	item.Text = comment.Text
	item.CommentActor = comment.Actor
	item.CommentActorURN = comment.ActorURN
	item.CommentActorName = comment.ActorName
	item.CommentedOnText = parentText
	if !comment.CreatedAt.IsZero() {
		item.CreatedAt = comment.CreatedAt
	}
	if item.CommentActor != nil {
		item.Actor = item.CommentActor
	}
	if item.CommentActorURN != "" {
		item.ActorURN = item.CommentActorURN
	}
	if item.CommentActorName != "" {
		item.ActorName = item.CommentActorName
	}
	if commentedOnURN := commentedOnURNFromCommentURN(comment.URN); commentedOnURN != "" {
		item.CommentedOnURN = commentedOnURN
		item.CommentedOnURL = feedUpdateURLFromURN(commentedOnURN)
		item.URL = firstNonEmpty(commentActivityURL(commentedOnURN, comment.URN), item.CommentedOnURL)
	}
	clearInapplicableActivityDetails(item)
}

func findGraphQLCommentEntity(data json.RawMessage, rawURN string, commentIndex graphQLCommentIndex) (graphQLCommentEntity, bool) {
	for _, key := range graphQLCommentLookupKeys(data, rawURN) {
		if comment, ok := commentIndex.entities[key]; ok {
			return selectGraphQLCommentReply(&comment, commentIndex), true
		}
		for _, referenceKey := range commentIndex.references[key] {
			if comment, ok := commentIndex.entities[referenceKey]; ok {
				return selectGraphQLCommentReply(&comment, commentIndex), true
			}
		}
	}

	for _, key := range graphQLCommentReferenceLookupKeys(data, commentIndex.references) {
		if comment, ok := commentIndex.entities[key]; ok {
			return selectGraphQLCommentReply(&comment, commentIndex), true
		}
	}

	return graphQLCommentEntity{}, false
}

func selectGraphQLCommentReply(comment *graphQLCommentEntity, commentIndex graphQLCommentIndex) graphQLCommentEntity {
	replyKeys := commentIndex.references[comment.SocialURN]
	if len(replyKeys) == 0 {
		return *comment
	}

	replies := graphQLCommentEntitiesForKeys(replyKeys, commentIndex.entities)
	if len(replies) == 0 {
		return *comment
	}
	if preferredURN := socialDetailReplyURN(comment.SocialURN, replies); preferredURN != "" {
		for i := range replies {
			if replies[i].URN == preferredURN || replies[i].EntityURN == preferredURN {
				return replies[i]
			}
		}
	}
	return *comment
}

func graphQLCommentEntitiesForKeys(keys []string, entities map[string]graphQLCommentEntity) []graphQLCommentEntity {
	replies := make([]graphQLCommentEntity, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		reply, ok := entities[key]
		if !ok || reply.URN == "" {
			continue
		}
		if _, exists := seen[reply.URN]; exists {
			continue
		}
		seen[reply.URN] = struct{}{}
		replies = append(replies, reply)
	}

	return replies
}

func graphQLCommentLookupKeys(data json.RawMessage, rawURN string) []string {
	keys := graphQLCommentKeysFromData(data)
	keys = appendUniqueStrings(keys, highlightedCommentKeysFromData(data)...)
	if activityURN := normalizeActivityURN(rawURN); activityURN != "" {
		keys = appendUniqueStrings(keys, strings.TrimPrefix(activityURN, "urn:li:activity:"))
	}

	return keys
}

func highlightedCommentKeysFromData(data json.RawMessage) []string {
	var entity struct {
		HighlightedComments []string `json:"*highlightedComments"`
	}
	if err := json.Unmarshal(data, &entity); err != nil {
		return nil
	}

	keys := make([]string, 0, len(entity.HighlightedComments)*2)
	for _, highlightedComment := range entity.HighlightedComments {
		keys = appendUniqueStrings(keys, commentLookupKeysFromURN(highlightedComment)...)
	}

	return keys
}

func graphQLCommentReferenceLookupKeys(data json.RawMessage, references map[string][]string) []string {
	keys := make([]string, 0)
	var value any
	if err := json.Unmarshal(data, &value); err == nil {
		keys = appendGraphQLReferenceLookupKeys(keys, value, references)
	}

	return keys
}

func appendGraphQLReferenceLookupKeys(keys []string, value any, references map[string][]string) []string {
	switch typedValue := value.(type) {
	case map[string]any:
		for _, child := range typedValue {
			keys = appendGraphQLReferenceLookupKeys(keys, child, references)
		}
	case []any:
		for _, child := range typedValue {
			keys = appendGraphQLReferenceLookupKeys(keys, child, references)
		}
	case string:
		keys = appendUniqueStrings(keys, references[typedValue]...)
	}

	return keys
}

func graphQLCommentKeysFromData(data json.RawMessage) []string {
	keys := make([]string, 0)
	var value any
	if err := json.Unmarshal(data, &value); err == nil {
		keys = appendGraphQLCommentLookupKeys(keys, value)
	}

	return keys
}

func appendGraphQLCommentLookupKeys(keys []string, value any) []string {
	switch typedValue := value.(type) {
	case map[string]any:
		for _, child := range typedValue {
			keys = appendGraphQLCommentLookupKeys(keys, child)
		}
	case []any:
		for _, child := range typedValue {
			keys = appendGraphQLCommentLookupKeys(keys, child)
		}
	case string:
		for _, urn := range commentURNsFromText(typedValue) {
			keys = appendUniqueStrings(keys, commentLookupKeysFromURN(urn)...)
		}
		for _, urn := range fsdCommentURNsFromText(typedValue) {
			keys = appendUniqueStrings(keys, commentLookupKeysFromURN(urn)...)
		}
	}

	return keys
}

func graphQLProfileUpdateElements(resp *VoyagerResponse, collectionName string) ([]json.RawMessage, string, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(resp.Data, &root); err != nil {
		return nil, "", err
	}

	page, err := graphQLProfileUpdatePage(root, collectionName)
	if err != nil {
		return nil, "", err
	}

	if len(page.Elements) > 0 {
		return page.Elements, page.Metadata.PaginationToken, nil
	}

	includedByURN := indexIncludedByURN(resp.Included)
	elements := make([]json.RawMessage, 0, len(page.ReferencedElements))
	for _, urn := range page.ReferencedElements {
		if raw, ok := includedByURN[urn]; ok {
			elements = append(elements, raw)
		}
	}

	return elements, page.Metadata.PaginationToken, nil
}

func graphQLProfileUpdatePage(root map[string]json.RawMessage, collectionName string) (profileUpdatesPage, error) {
	if rawPage := root[collectionName]; len(rawPage) > 0 {
		var page profileUpdatesPage
		if err := json.Unmarshal(rawPage, &page); err != nil {
			return profileUpdatesPage{}, err
		}
		if page.hasElements() {
			return page, nil
		}
	}

	if rawData := root["data"]; len(rawData) > 0 {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(rawData, &nested); err != nil {
			return profileUpdatesPage{}, err
		}
		if rawPage := nested[collectionName]; len(rawPage) > 0 {
			var page profileUpdatesPage
			if err := json.Unmarshal(rawPage, &page); err != nil {
				return profileUpdatesPage{}, err
			}
			return page, nil
		}
	}

	return profileUpdatesPage{}, nil
}

type profileUpdatesPage struct {
	Elements           []json.RawMessage `json:"elements"`
	ReferencedElements []string          `json:"*elements"`
	Metadata           struct {
		PaginationToken string `json:"paginationToken"`
	} `json:"metadata"`
}

func (p profileUpdatesPage) hasElements() bool {
	return p.Elements != nil || p.ReferencedElements != nil
}

func parseGraphQLProfileUpdate(data json.RawMessage, category RecentActivityCategory) (*ActivityItem, error) {
	var entity struct {
		Type      string `json:"$type"`
		EntityURN string `json:"entityUrn"`
		URN       string `json:"urn"`
		Metadata  struct {
			BackendURN string `json:"backendUrn"`
			ShareURN   string `json:"shareUrn"`
		} `json:"metadata"`
		Commentary struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
		} `json:"commentary"`
		SocialContent struct {
			ShareURL string `json:"shareUrl"`
		} `json:"socialContent"`
		Actor struct {
			URN  string `json:"urn"`
			Name struct {
				Text string `json:"text"`
			} `json:"name"`
		} `json:"actor"`
		ActorURN     string `json:"*actor"`
		CreatedAt    int64  `json:"createdAt"`
		PublishedAt  int64  `json:"publishedAt"`
		SocialDetail struct {
			TotalSocialActivityCounts struct {
				NumLikes    int `json:"numLikes"`
				NumComments int `json:"numComments"`
				NumShares   int `json:"numShares"`
			} `json:"totalSocialActivityCounts"`
		} `json:"socialDetail"`
	}
	if err := json.Unmarshal(data, &entity); err != nil {
		return nil, err
	}
	if !isActivityType(entity.Type) {
		return nil, fmt.Errorf("unsupported activity entity type: %s", entity.Type)
	}

	rawURN := firstNonEmpty(entity.EntityURN, entity.URN)
	details := parseActivityDetails(data)
	activityURN := normalizeActivityURN(entity.Metadata.BackendURN)
	if activityURN == "" {
		activityURN = normalizeActivityURN(rawURN)
	}
	if activityURN == "" {
		return nil, fmt.Errorf("no URN in activity item")
	}

	item := &ActivityItem{
		URN:              activityURN,
		Type:             entity.Type,
		Actor:            activityActorPtrFromRawMessage(data),
		ActorURN:         firstNonEmpty(entity.Actor.URN, entity.ActorURN),
		ActorName:        entity.Actor.Name.Text,
		Text:             entity.Commentary.Text.Text,
		URL:              firstNonEmpty(entity.SocialContent.ShareURL, activityURLFromURN(activityURN)),
		RawURN:           rawURN,
		LikeCount:        entity.SocialDetail.TotalSocialActivityCounts.NumLikes,
		CommentCount:     entity.SocialDetail.TotalSocialActivityCounts.NumComments,
		ShareCount:       entity.SocialDetail.TotalSocialActivityCounts.NumShares,
		ContentCategory:  category,
		ReactionType:     details.ReactionType,
		ReactionURN:      details.ReactionURN,
		ReactionActor:    details.ReactionActor,
		ReactionActorURN: details.ReactionActorURN,
		ReactedToURN:     details.ReactedToURN,
		ReactedToURL:     details.ReactedToURL,
		CommentURN:       details.CommentURN,
		CommentActor:     details.CommentActor,
		CommentActorURN:  details.CommentActorURN,
		CommentActorName: details.CommentActorName,
		CommentText:      details.CommentText,
		CommentedOnURN:   details.CommentedOnURN,
		CommentedOnURL:   details.CommentedOnURL,
		CommentedOnText:  details.CommentedOnText,
	}
	if item.ContentCategory == "" {
		item.ContentCategory = RecentActivityCategoryPosts
	}
	populateActivityActorCompatibility(item)
	if item.CommentActorURN != "" {
		if item.CommentActor != nil {
			item.Actor = item.CommentActor
		}
		item.ActorURN = item.CommentActorURN
	}
	if item.CommentActorName != "" {
		item.ActorName = item.CommentActorName
	}
	if item.CommentText != "" {
		item.Text = item.CommentText
	}
	if entity.CreatedAt > 0 {
		item.CreatedAt = time.Unix(entity.CreatedAt/1000, 0)
	} else if entity.PublishedAt > 0 {
		item.CreatedAt = time.Unix(entity.PublishedAt/1000, 0)
	}
	clearInapplicableActivityDetails(item)
	return item, nil
}

func isActivityCandidate(data json.RawMessage) bool {
	var entity struct {
		Type string `json:"$type"`
	}
	if err := json.Unmarshal(data, &entity); err != nil {
		return false
	}

	return isActivityType(entity.Type)
}

func buildActivityDebugShape(endpoint recentActivityEndpoint, status int, body []byte) (*ActivityDebugShape, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, &Error{Code: ErrCodeServerError, Message: fmt.Sprintf("failed to decode response: %v", err)}
	}

	shape := &ActivityDebugShape{
		EndpointPath:  endpoint.path,
		Query:         safeEndpointQuery(endpoint),
		Status:        status,
		TopLevelKeys:  sortedJSONKeys(root),
		DataCount:     debugDataCount(root["data"]),
		IncludedCount: debugArrayCount(root["included"]),
		ExampleTypes:  debugExampleTypes(root, 10),
		PagingKeys:    debugPagingKeys(root["paging"]),
		HasNextLink:   debugHasNextLink(root["paging"]),
	}

	return shape, nil
}

func safeEndpointQuery(endpoint recentActivityEndpoint) []string {
	if endpoint.rawQuery != "" {
		return safeRawQueryPairs(endpoint.rawQuery)
	}

	return safeQueryPairs(endpoint.query)
}

func safeRawQueryPairs(rawQuery string) []string {
	pairs := strings.Split(rawQuery, "&")
	safePairs := pairs[:0]
	for _, pair := range pairs {
		key := pair
		if index := strings.Index(pair, "="); index >= 0 {
			key = pair[:index]
		}
		if !isSensitiveField(key) {
			safePairs = append(safePairs, pair)
		}
	}

	return safePairs
}

func safeQueryPairs(query url.Values) []string {
	pairs := make([]string, 0)
	keys := make([]string, 0, len(query))
	for key := range query {
		if isSensitiveField(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		values := append([]string(nil), query[key]...)
		sort.Strings(values)
		for _, value := range values {
			pairs = append(pairs, fmt.Sprintf("%s=%s", key, value))
		}
	}

	return pairs
}

func sortedJSONKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func debugDataCount(data json.RawMessage) int {
	if len(data) == 0 {
		return 0
	}
	for _, collectionName := range []string{
		"feedDashProfileUpdatesByMemberShareFeed",
		"feedDashProfileUpdatesByMemberComments",
		"feedDashProfileUpdatesByMemberReactions",
	} {
		if elements, _, err := graphQLProfileUpdateElements(&VoyagerResponse{Data: data}, collectionName); err == nil && len(elements) > 0 {
			return len(elements)
		}
	}

	var dataObject struct {
		Elements     []json.RawMessage `json:"elements"`
		StarElements []string          `json:"*elements"`
	}
	if err := json.Unmarshal(data, &dataObject); err == nil {
		if dataObject.Elements != nil {
			return len(dataObject.Elements)
		}
		if dataObject.StarElements != nil {
			return len(dataObject.StarElements)
		}
	}

	var dataArray []json.RawMessage
	if err := json.Unmarshal(data, &dataArray); err == nil {
		return len(dataArray)
	}

	return 1
}

func debugArrayCount(data json.RawMessage) int {
	if len(data) == 0 {
		return 0
	}

	var elements []json.RawMessage
	if err := json.Unmarshal(data, &elements); err != nil {
		return 0
	}

	return len(elements)
}

func debugExampleTypes(root map[string]json.RawMessage, limit int) []string {
	seen := make(map[string]struct{})
	types := make([]string, 0, limit)
	collectDebugTypes(root["data"], seen, &types, limit)
	collectDebugTypes(root["included"], seen, &types, limit)
	return types
}

func collectDebugTypes(data json.RawMessage, seen map[string]struct{}, types *[]string, limit int) {
	if len(data) == 0 || len(*types) >= limit {
		return
	}

	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return
	}
	walkDebugTypes(value, seen, types, limit)
}

func walkDebugTypes(value any, seen map[string]struct{}, types *[]string, limit int) {
	if len(*types) >= limit {
		return
	}

	switch typedValue := value.(type) {
	case map[string]any:
		if rawType, ok := typedValue["$type"].(string); ok && rawType != "" {
			if _, exists := seen[rawType]; !exists {
				seen[rawType] = struct{}{}
				*types = append(*types, rawType)
			}
		}
		keys := make([]string, 0, len(typedValue))
		for key := range typedValue {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			walkDebugTypes(typedValue[key], seen, types, limit)
		}
	case []any:
		for _, element := range typedValue {
			walkDebugTypes(element, seen, types, limit)
		}
	}
}

func debugPagingKeys(paging json.RawMessage) []string {
	if len(paging) == 0 {
		return nil
	}

	var pagingObject map[string]json.RawMessage
	if err := json.Unmarshal(paging, &pagingObject); err != nil {
		return nil
	}

	return sortedJSONKeys(pagingObject)
}

func debugHasNextLink(paging json.RawMessage) bool {
	if len(paging) == 0 {
		return false
	}

	var pagingObject struct {
		Links []struct {
			Rel string `json:"rel"`
		} `json:"links"`
	}
	if err := json.Unmarshal(paging, &pagingObject); err != nil {
		return false
	}

	for _, link := range pagingObject.Links {
		if strings.EqualFold(link.Rel, "next") {
			return true
		}
	}

	return false
}

func isSensitiveField(field string) bool {
	switch strings.ToLower(field) {
	case "li_at", "jsessionid", "csrf-token", "csrf_token", "authorization", "cookie":
		return true
	default:
		return false
	}
}

// parseProfileActivityFromResponse extracts profile activity as feed-compatible items.
func parseProfileActivityFromResponse(resp *VoyagerResponse) ([]FeedItem, error) {
	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		return nil, err
	}

	return activityItemsToFeedItems(items), nil
}

func primaryActivityElements(data json.RawMessage, included []json.RawMessage) []json.RawMessage {
	includedByURN := indexIncludedByURN(included)
	elements := make([]json.RawMessage, 0, len(included)+1)

	if len(data) > 0 {
		dataElements, referencedURNs := activityElementsFromData(data)
		elements = append(elements, dataElements...)
		for _, urn := range referencedURNs {
			if raw, ok := includedByURN[urn]; ok {
				elements = append(elements, raw)
			}
		}
	}

	return elements
}

func collectActivityEntities(resp *VoyagerResponse) map[string]ActivityItem {
	activityEntities := make(map[string]ActivityItem)
	for _, raw := range resp.Included {
		item, err := parseActivityEntity(raw)
		if err != nil || item.URN == "" {
			continue
		}

		activityEntities[item.URN] = *item
	}

	return activityEntities
}

func activityElementsFromData(data json.RawMessage) (elements []json.RawMessage, referencedURNs []string) {
	var dataResp struct {
		Elements          []json.RawMessage `json:"elements"`
		ReferencedElement []string          `json:"*elements"`
	}
	if err := json.Unmarshal(data, &dataResp); err == nil {
		if dataResp.Elements != nil || dataResp.ReferencedElement != nil {
			return dataResp.Elements, dataResp.ReferencedElement
		}
	}

	return []json.RawMessage{data}, nil
}

func indexIncludedByURN(included []json.RawMessage) map[string]json.RawMessage {
	indexed := make(map[string]json.RawMessage, len(included))
	for _, raw := range included {
		var entity struct {
			EntityURN string `json:"entityUrn"`
			URN       string `json:"urn"`
		}
		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}

		for _, urn := range []string{entity.EntityURN, entity.URN, normalizeActivityURN(entity.EntityURN), normalizeActivityURN(entity.URN)} {
			if urn != "" {
				indexed[urn] = raw
			}
		}
	}

	return indexed
}

func dedupeActivityItems(items []ActivityItem) []ActivityItem {
	seen := make(map[string]struct{}, len(items))
	uniqueItems := make([]ActivityItem, 0, len(items))

	for i := range items {
		item := &items[i]
		if item.URN == "" {
			uniqueItems = append(uniqueItems, *item)
			continue
		}

		if _, ok := seen[item.URN]; ok {
			continue
		}
		seen[item.URN] = struct{}{}
		uniqueItems = append(uniqueItems, *item)
	}

	return uniqueItems
}

func parseActivityItem(data json.RawMessage, activityEntities map[string]ActivityItem) (*ActivityItem, error) {
	item, err := parseActivityEntity(data)
	if err != nil {
		return nil, err
	}

	if includedItem, ok := activityEntities[item.URN]; ok {
		mergeActivityItem(item, &includedItem)
	}

	return item, nil
}

func parseActivityEntity(data json.RawMessage) (*ActivityItem, error) {
	var entity struct {
		Type      string `json:"$type"`
		EntityURN string `json:"entityUrn"`
		URN       string `json:"urn"`
		Actor     struct {
			URN  string `json:"urn"`
			Name struct {
				Text string `json:"text"`
			} `json:"name"`
		} `json:"actor"`
		ActorURN   string `json:"*actor"`
		Commentary struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
		} `json:"commentary"`
		CommentaryV2 struct {
			Text string `json:"text"`
		} `json:"commentaryV2"`
		Text struct {
			Text string `json:"text"`
		} `json:"text"`
		SocialDetail struct {
			LikesCount    int `json:"likes,omitempty"`
			CommentsCount int `json:"comments,omitempty"`
			SharesCount   int `json:"shares,omitempty"`
		} `json:"socialDetail"`
		SocialActivityCounts struct {
			NumLikes    int `json:"numLikes"`
			NumComments int `json:"numComments"`
			NumShares   int `json:"numShares"`
		} `json:"socialActivityCounts"`
		CreatedAt   int64  `json:"createdAt"`
		PublishedAt int64  `json:"publishedAt"`
		URL         string `json:"url"`
	}

	if err := json.Unmarshal(data, &entity); err != nil {
		return nil, err
	}

	if !isActivityType(entity.Type) {
		return nil, fmt.Errorf("unsupported activity entity type: %s", entity.Type)
	}

	rawURN := firstNonEmpty(entity.EntityURN, entity.URN)
	details := parseActivityDetails(data)
	urn := recentActivityItemURN(rawURN, &details)
	if urn == "" {
		return nil, fmt.Errorf("no URN in activity item")
	}

	item := &ActivityItem{
		URN:              urn,
		Type:             entity.Type,
		Actor:            activityActorPtrFromRawMessage(data),
		ActorURN:         firstNonEmpty(entity.Actor.URN, entity.ActorURN),
		ActorName:        entity.Actor.Name.Text,
		Text:             firstNonEmpty(entity.Commentary.Text.Text, entity.CommentaryV2.Text, entity.Text.Text),
		LikeCount:        firstNonZero(entity.SocialDetail.LikesCount, entity.SocialActivityCounts.NumLikes),
		CommentCount:     firstNonZero(entity.SocialDetail.CommentsCount, entity.SocialActivityCounts.NumComments),
		ShareCount:       firstNonZero(entity.SocialDetail.SharesCount, entity.SocialActivityCounts.NumShares),
		URL:              firstNonEmpty(entity.URL, commentActivityURL(details.CommentedOnURN, details.CommentURN), activityURLFromURN(urn), details.CommentedOnURL),
		RawURN:           rawURN,
		ContentCategory:  classifyRecentActivityContent(data),
		ReactionType:     details.ReactionType,
		ReactionURN:      details.ReactionURN,
		ReactionActor:    details.ReactionActor,
		ReactionActorURN: details.ReactionActorURN,
		ReactedToURN:     details.ReactedToURN,
		ReactedToURL:     details.ReactedToURL,
		CommentURN:       details.CommentURN,
		CommentActor:     details.CommentActor,
		CommentActorURN:  details.CommentActorURN,
		CommentActorName: details.CommentActorName,
		CommentText:      details.CommentText,
		CommentedOnURN:   details.CommentedOnURN,
		CommentedOnURL:   details.CommentedOnURL,
		CommentedOnText:  details.CommentedOnText,
	}
	populateActivityActorCompatibility(item)
	if item.CommentActorURN != "" {
		if item.CommentActor != nil {
			item.Actor = item.CommentActor
		}
		item.ActorURN = item.CommentActorURN
	}
	if item.CommentActorName != "" {
		item.ActorName = item.CommentActorName
	}
	if item.CommentText != "" {
		item.Text = item.CommentText
	}
	clearInapplicableActivityDetails(item)
	if item.Type == "" {
		item.Type = "activity"
	}
	if entity.CreatedAt > 0 {
		item.CreatedAt = time.Unix(entity.CreatedAt/1000, 0)
	} else if entity.PublishedAt > 0 {
		item.CreatedAt = time.Unix(entity.PublishedAt/1000, 0)
	}

	return item, nil
}

func mergeActivityItem(item, includedItem *ActivityItem) {
	item.hasLookupDetails = true
	item.RawURN = firstNonEmpty(item.RawURN, includedItem.RawURN)
	if item.ActorURN == "" {
		item.ActorURN = includedItem.ActorURN
	}
	if item.ActorName == "" {
		item.ActorName = includedItem.ActorName
	}
	if item.Actor == nil {
		item.Actor = includedItem.Actor
	}
	if item.Text == "" {
		item.Text = includedItem.Text
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = includedItem.CreatedAt
	}
	if item.LikeCount == 0 {
		item.LikeCount = includedItem.LikeCount
	}
	if item.CommentCount == 0 {
		item.CommentCount = includedItem.CommentCount
	}
	if item.ShareCount == 0 {
		item.ShareCount = includedItem.ShareCount
	}
	if commentURL := commentActivityURL(firstNonEmpty(item.CommentedOnURN, includedItem.CommentedOnURN), firstNonEmpty(item.CommentURN, includedItem.CommentURN)); commentURL != "" {
		item.URL = commentURL
	} else if item.URL == "" {
		item.URL = includedItem.URL
	}
	if item.ContentCategory == "" {
		item.ContentCategory = includedItem.ContentCategory
	}
	if item.ReactionType == "" {
		item.ReactionType = includedItem.ReactionType
	}
	if item.ReactionURN == "" {
		item.ReactionURN = includedItem.ReactionURN
	}
	if item.ReactionActorURN == "" {
		item.ReactionActorURN = includedItem.ReactionActorURN
	}
	if item.ReactionActor == nil {
		item.ReactionActor = includedItem.ReactionActor
	}
	if item.ReactedToURN == "" {
		item.ReactedToURN = includedItem.ReactedToURN
	}
	if item.ReactedToURL == "" {
		item.ReactedToURL = includedItem.ReactedToURL
	}
	if item.CommentURN == "" {
		item.CommentURN = includedItem.CommentURN
	}
	if item.CommentActorURN == "" {
		item.CommentActorURN = includedItem.CommentActorURN
	}
	if item.CommentActorName == "" {
		item.CommentActorName = includedItem.CommentActorName
	}
	if item.CommentActor == nil {
		item.CommentActor = includedItem.CommentActor
	}
	if item.CommentText == "" {
		item.CommentText = includedItem.CommentText
	}
	if item.CommentedOnURN == "" {
		item.CommentedOnURN = includedItem.CommentedOnURN
	}
	if item.CommentedOnURL == "" {
		item.CommentedOnURL = includedItem.CommentedOnURL
	}
	if item.CommentedOnText == "" {
		item.CommentedOnText = includedItem.CommentedOnText
	}
	populateActivityActorCompatibility(item)
	clearInapplicableActivityDetails(item)
}

func clearInapplicableActivityDetails(item *ActivityItem) {
	switch item.ContentCategory {
	case RecentActivityCategoryComments:
		item.ReactionType = ""
		item.ReactionURN = ""
		item.ReactionActor = nil
		item.ReactionActorURN = ""
		item.ReactedToURN = ""
		item.ReactedToURL = ""
	case RecentActivityCategoryReactions:
		item.CommentURN = ""
		item.CommentActor = nil
		item.CommentActorURN = ""
		item.CommentActorName = ""
		item.CommentText = ""
		item.CommentedOnURN = ""
		item.CommentedOnURL = ""
		item.CommentedOnText = ""
	case "",
		RecentActivityCategoryAll,
		RecentActivityCategoryPosts,
		RecentActivityCategoryImages,
		RecentActivityCategoryVideos,
		RecentActivityCategoryDocuments,
		RecentActivityCategoryEvents:
	}
}

func filterRecentActivityByCategory(items []ActivityItem, category RecentActivityCategory) []ActivityItem {
	if category == RecentActivityCategoryAll {
		return items
	}

	filtered := make([]ActivityItem, 0, len(items))
	for i := range items {
		item := &items[i]
		if itemMatchesRecentActivityCategory(item, category) {
			filtered = append(filtered, *item)
		}
	}

	return filtered
}

func itemMatchesRecentActivityCategory(item *ActivityItem, category RecentActivityCategory) bool {
	if category == RecentActivityCategoryPosts {
		return item.ContentCategory == "" && isPostLikePrimaryActivity(item)
	}

	return item.ContentCategory == category
}

func isPostLikePrimaryActivity(item *ActivityItem) bool {
	if item == nil {
		return false
	}
	if isUnresolvedWrapperOnlyActivity(item) {
		return false
	}

	typeText := strings.ToLower(item.Type)
	return strings.Contains(typeText, "update") || strings.Contains(typeText, "activity")
}

func isUnresolvedWrapperOnlyActivity(item *ActivityItem) bool {
	return item.RawURN != item.URN &&
		!item.hasLookupDetails &&
		item.ActorURN == "" &&
		item.ActorName == "" &&
		item.Text == "" &&
		item.CreatedAt.IsZero()
}

func classifyRecentActivityContent(data json.RawMessage) RecentActivityCategory {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return ""
	}

	classifier := &activityContentClassifier{}
	classifier.visit(nil, value)
	return classifier.category()
}

func parseActivityDetails(data json.RawMessage) ActivityItem {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return ActivityItem{}
	}

	details := &activityDetailParser{}
	details.visit(nil, value)
	return details.item
}

func recentActivityItemURN(rawURN string, details *ActivityItem) string {
	if details.CommentURN != "" {
		return details.CommentURN
	}

	return normalizeActivityURN(rawURN)
}

type activityDetailParser struct {
	item ActivityItem
}

func (p *activityDetailParser) visit(path []string, value any) {
	switch typedValue := value.(type) {
	case map[string]any:
		p.captureCommentObject(path, typedValue)
		p.captureReactionObject(path, typedValue)
		for key, child := range typedValue {
			childPath := make([]string, 0, len(path)+1)
			childPath = append(childPath, path...)
			childPath = append(childPath, key)
			p.captureSignal(childPath, key, child)
			p.visit(childPath, child)
		}
	case []any:
		for _, child := range typedValue {
			p.visit(path, child)
		}
	}
}

func (p *activityDetailParser) captureSignal(path []string, key string, value any) {
	valueText, ok := value.(string)
	if !ok || valueText == "" {
		return
	}

	keyText := strings.ToLower(key)
	pathText := strings.ToLower(strings.Join(path, "."))
	switch {
	case keyText == "reactiontype" && p.item.ReactionType == "":
		p.item.ReactionType = valueText
	case isReactionURNKey(keyText) && strings.HasPrefix(valueText, "urn:li:reaction:") && p.item.ReactionURN == "":
		p.item.ReactionURN = valueText
		p.captureReactionURN(valueText)
	case isReactionActorPath(pathText, keyText) && p.item.ReactionActorURN == "":
		p.item.ReactionActorURN = valueText
	case isCommentURNField(keyText) && p.item.CommentURN == "":
		if urn := firstCommentURNFromText(valueText); urn != "" {
			p.item.CommentURN = urn
			p.captureCommentURN(urn)
		} else if urn := firstFSDCommentURNFromText(valueText); urn != "" {
			p.item.CommentURN = canonicalCommentURNFromFSDCommentURN(urn)
			p.captureCommentURN(p.item.CommentURN)
		}
	case isCommentActorPath(pathText, keyText) && p.item.CommentActorURN == "":
		p.item.CommentActorURN = valueText
	case isCommentTextPath(pathText, keyText) && p.item.CommentText == "":
		p.item.CommentText = valueText
	case isCommentedOnPath(pathText, keyText) && p.item.CommentedOnURN == "":
		if urn := normalizeCommentParentURN(valueText); urn != "" {
			p.item.CommentedOnURN = urn
			p.item.CommentedOnURL = feedUpdateURLFromURN(urn)
		}
	}
}

func (p *activityDetailParser) captureCommentObject(path []string, object map[string]any) {
	if !isExplicitCommentObject(path, object) {
		return
	}

	if p.item.CommentURN == "" {
		commentURNValue := stringField(object, "entityUrn", "urn", "commentUrn")
		if urn := firstCommentURNFromText(commentURNValue); urn != "" {
			p.item.CommentURN = urn
			p.captureCommentURN(urn)
		} else if urn := firstFSDCommentURNFromText(commentURNValue); urn != "" {
			p.item.CommentURN = canonicalCommentURNFromFSDCommentURN(urn)
			p.captureCommentURN(p.item.CommentURN)
		}
	}
	if p.item.CommentActorURN == "" {
		p.item.CommentActorURN = actorURNFromObject(object)
	}
	if p.item.CommentActorName == "" {
		p.item.CommentActorName = actorNameFromObject(object)
	}
	if p.item.CommentActor == nil {
		p.item.CommentActor = activityActorPtrFromObject(object)
	}
	if p.item.CommentText == "" {
		p.item.CommentText = textFromObject(object)
	}
	if p.item.CommentedOnURN == "" {
		if urn := normalizeCommentParentURN(stringField(object, "commentedOnUrn", "objectUrn", "threadUrn", "activityUrn")); urn != "" {
			p.item.CommentedOnURN = urn
			p.item.CommentedOnURL = feedUpdateURLFromURN(urn)
		}
	}
}

func (p *activityDetailParser) captureReactionObject(path []string, object map[string]any) {
	if !isExplicitReactionObject(path, object) {
		return
	}

	if p.item.ReactionURN == "" {
		if urn := stringField(object, "entityUrn", jsonURNKey, "reactionUrn"); strings.HasPrefix(urn, "urn:li:reaction:") {
			p.item.ReactionURN = urn
			p.captureReactionURN(urn)
		}
	}
	if p.item.ReactionType == "" {
		p.item.ReactionType = stringField(object, "reactionType")
	}
	if p.item.ReactionActorURN == "" {
		p.item.ReactionActorURN = actorURNFromObject(object)
	}
	if p.item.ReactionActor == nil {
		p.item.ReactionActor = activityActorPtrFromConfirmedActorObject(object)
	}
}

func (p *activityDetailParser) captureReactionURN(urn string) {
	matches := reactionURNPattern.FindStringSubmatch(urn)
	if len(matches) != 3 {
		return
	}
	p.item.ReactionActorURN = matches[1]
	p.item.ReactedToURN = matches[2]
	p.item.ReactedToURL = activityURLFromURN(matches[2])
}

func (p *activityDetailParser) captureCommentURN(urn string) {
	commentedOnURN := commentedOnURNFromCommentURN(urn)
	if commentedOnURN == "" {
		return
	}
	p.item.CommentedOnURN = commentedOnURN
	p.item.CommentedOnURL = feedUpdateURLFromURN(commentedOnURN)
}

func commentedOnURNFromCommentURN(urn string) string {
	matches := commentURNPattern.FindStringSubmatch(urn)
	if len(matches) != 3 {
		return ""
	}

	return normalizeCommentParentURN(matches[1])
}

func commentIDFromURN(urn string) string {
	matches := commentURNPattern.FindStringSubmatch(urn)
	if len(matches) != 3 {
		return ""
	}

	return matches[2]
}

func commentURNsFromText(text string) []string {
	matches := commentURNPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	return nonEmptyUniqueStrings(matches...)
}

func firstCommentURNFromText(text string) string {
	for _, urn := range commentURNsFromText(text) {
		return urn
	}

	return ""
}

func fsdCommentURNsFromText(text string) []string {
	matches := fsdCommentPattern.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}

	return nonEmptyUniqueStrings(matches...)
}

func firstFSDCommentURNFromText(text string) string {
	for _, urn := range fsdCommentURNsFromText(text) {
		return urn
	}

	return ""
}

func canonicalCommentURNFromFSDCommentURN(urn string) string {
	matches := fsdCommentPattern.FindStringSubmatch(urn)
	if len(matches) != 3 {
		return ""
	}

	parentURN := normalizeCommentParentURN(matches[2])
	if parentURN == "" {
		return ""
	}

	return fmt.Sprintf("urn:li:comment:(%s,%s)", canonicalCommentParentComponent(parentURN), matches[1])
}

func commentLookupKeysFromURN(urn string) []string {
	keys := nonEmptyUniqueStrings(urn)
	if canonicalURN := canonicalCommentURNFromFSDCommentURN(urn); canonicalURN != "" {
		keys = appendUniqueStrings(keys, canonicalURN)
		if id := commentIDFromURN(canonicalURN); id != "" {
			keys = appendUniqueStrings(keys, id)
		}
		return keys
	}
	if id := commentIDFromURN(urn); id != "" {
		keys = appendUniqueStrings(keys, id)
	}

	return keys
}

func canonicalCommentParentComponent(parentURN string) string {
	if strings.HasPrefix(parentURN, "urn:li:activity:") {
		return "activity:" + strings.TrimPrefix(parentURN, "urn:li:activity:")
	}
	if strings.HasPrefix(parentURN, "urn:li:ugcPost:") {
		return "ugcPost:" + strings.TrimPrefix(parentURN, "urn:li:ugcPost:")
	}

	return parentURN
}

func socialDetailReplyURN(urn string, candidates []graphQLCommentEntity) string {
	for _, commentURN := range socialDetailCommentURNs(urn) {
		for i := range candidates {
			if candidates[i].URN == commentURN || candidates[i].EntityURN == commentURN {
				return candidates[i].URN
			}
		}
	}

	return ""
}

func socialDetailCommentURNs(urn string) []string {
	commentURNs := make([]string, 0)
	for _, commentURN := range commentURNsFromText(urn) {
		commentURNs = appendUniqueStrings(commentURNs, commentURN)
	}
	for _, fsdCommentURN := range fsdCommentURNsFromText(urn) {
		commentURNs = appendUniqueStrings(commentURNs, fsdCommentURN)
		if commentURN := canonicalCommentURNFromFSDCommentURN(fsdCommentURN); commentURN != "" {
			commentURNs = appendUniqueStrings(commentURNs, commentURN)
		}
	}

	return commentURNs
}

func normalizeCommentParentURN(parent string) string {
	if strings.HasPrefix(parent, "urn:li:activity:") {
		return parent
	}
	if strings.HasPrefix(parent, "activity:") {
		return "urn:li:activity:" + strings.TrimPrefix(parent, "activity:")
	}
	if strings.HasPrefix(parent, "urn:li:ugcPost:") {
		return parent
	}
	if strings.HasPrefix(parent, "ugcPost:") {
		return "urn:li:ugcPost:" + strings.TrimPrefix(parent, "ugcPost:")
	}

	return ""
}

type activityContentClassifier struct {
	hasImage    bool
	hasVideo    bool
	hasDocument bool
	hasEvent    bool
	hasReaction bool
	hasComment  bool
}

func (c *activityContentClassifier) visit(path []string, value any) {
	switch typedValue := value.(type) {
	case map[string]any:
		for key, child := range typedValue {
			childPath := make([]string, 0, len(path)+1)
			childPath = append(childPath, path...)
			childPath = append(childPath, key)
			c.classifySignal(childPath, key, child)
			c.visit(childPath, child)
		}
	case []any:
		for _, child := range typedValue {
			c.visit(path, child)
		}
	}
}

func (c *activityContentClassifier) classifySignal(path []string, key string, value any) {
	keyText := strings.ToLower(key)
	pathText := strings.ToLower(strings.Join(path, "."))
	stringText, hasString := value.(string)
	valueText := strings.ToLower(stringText)
	combined := keyText + " " + valueText

	if c.hasReactionSignal(keyText, valueText) {
		c.hasReaction = true
	}
	if c.hasCommentSignal(pathText, keyText, valueText) {
		c.hasComment = true
	}
	if hasString && strings.Contains(valueText, "urn:li:event:") {
		c.hasEvent = true
	}
	if hasString && strings.Contains(valueText, "urn:li:document") {
		c.hasDocument = true
	}
	if hasString && strings.Contains(valueText, "urn:li:video") {
		c.hasVideo = true
	}
	if hasString && strings.Contains(valueText, "urn:li:image") {
		c.hasImage = true
	}

	if !isContentSignalPath(pathText) {
		return
	}

	if strings.Contains(combined, "event") {
		c.hasEvent = true
	}
	if strings.Contains(combined, "document") || strings.Contains(combined, "nativedocument") {
		c.hasDocument = true
	}
	if strings.Contains(combined, "video") || strings.Contains(combined, "playable") {
		c.hasVideo = true
	}
	if strings.Contains(combined, "image") || strings.Contains(combined, "photo") || strings.Contains(combined, "artifact") {
		c.hasImage = true
	}
}

func (c *activityContentClassifier) hasReactionSignal(keyText, valueText string) bool {
	return keyText == "reactiontype" ||
		isReactionURNKey(keyText) && strings.HasPrefix(valueText, "urn:li:reaction:")
}

func (c *activityContentClassifier) hasCommentSignal(pathText, keyText, valueText string) bool {
	return isCommentURNKey(keyText) && strings.HasPrefix(valueText, "urn:li:comment:") ||
		strings.HasPrefix(valueText, "urn:li:comment:") ||
		isCommentType(valueText) ||
		isCommentObjectPath(pathText) && (isCommentURNLikeKey(keyText) || keyText == commentMessageKey || keyText == "actor" || keyText == "created")
}

func (c *activityContentClassifier) category() RecentActivityCategory {
	switch {
	case c.hasComment:
		return RecentActivityCategoryComments
	case c.hasReaction:
		return RecentActivityCategoryReactions
	case c.hasEvent:
		return RecentActivityCategoryEvents
	case c.hasDocument:
		return RecentActivityCategoryDocuments
	case c.hasVideo:
		return RecentActivityCategoryVideos
	case c.hasImage:
		return RecentActivityCategoryImages
	default:
		return ""
	}
}

func isReactionURNKey(keyText string) bool {
	return keyText == "reactionurn" || keyText == "*reaction" || keyText == reactionKey
}

func isCommentURNKey(keyText string) bool {
	return keyText == "commenturn" || keyText == "*comment" || keyText == "comment"
}

func isCommentURNLikeKey(keyText string) bool {
	return keyText == jsonURNKey || keyText == "entityurn" || keyText == "commenturn" || keyText == "*comment"
}

func isCommentURNField(keyText string) bool {
	return isCommentURNKey(keyText) || isCommentURNLikeKey(keyText)
}

func isCommentType(valueText string) bool {
	return strings.HasSuffix(valueText, ".comment") ||
		strings.HasSuffix(valueText, ".commentary") ||
		strings.HasSuffix(valueText, ".socialcomment") ||
		strings.HasSuffix(valueText, ".commentupdate") ||
		strings.Contains(valueText, ".commentupdate")
}

func isCommentObjectPath(pathText string) bool {
	segments := strings.Split(pathText, ".")
	for _, segment := range segments {
		if segment == "comment" || segment == "comments" || strings.HasSuffix(segment, "comment") {
			return true
		}
	}

	return false
}

func isExplicitCommentObject(path []string, object map[string]any) bool {
	pathText := strings.ToLower(strings.Join(path, "."))
	if isCommentObjectPath(pathText) {
		return true
	}
	if len(path) != 0 {
		return false
	}

	if typeText := strings.ToLower(stringField(object, "$type")); isCommentType(typeText) {
		return true
	}
	return strings.HasPrefix(stringField(object, "entityUrn", jsonURNKey, "commentUrn"), "urn:li:comment:")
}

func isCommentActorPath(pathText, keyText string) bool {
	return isCommentObjectPath(pathText) && (keyText == "*actor" || keyText == "actorurn" || keyText == jsonURNKey)
}

func isReactionObjectPath(pathText string) bool {
	segments := strings.Split(pathText, ".")
	for _, segment := range segments {
		if segment == reactionKey || segment == "reactions" || strings.HasSuffix(segment, reactionKey) {
			return true
		}
	}

	return false
}

func isExplicitReactionObject(path []string, object map[string]any) bool {
	pathText := strings.ToLower(strings.Join(path, "."))
	if isReactionObjectPath(pathText) {
		return true
	}
	if len(path) != 0 {
		return false
	}

	return strings.HasPrefix(stringField(object, "entityUrn", jsonURNKey, "reactionUrn"), "urn:li:reaction:")
}

func isReactionActorPath(pathText, keyText string) bool {
	return isReactionObjectPath(pathText) && (keyText == "*actor" || keyText == "actorurn" || keyText == jsonURNKey)
}

func isCommentTextPath(pathText, keyText string) bool {
	if strings.Contains(pathText, ".actor.") || strings.Contains(pathText, ".name.") {
		return false
	}

	return isCommentObjectPath(pathText) && (keyText == commentMessageKey || keyText == "text")
}

func isCommentedOnPath(pathText, keyText string) bool {
	return isCommentObjectPath(pathText) && (keyText == "commentedonurn" || keyText == "objecturn" || keyText == "threadurn" || keyText == "activityurn")
}

func stringField(object map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := object[key].(string); ok && value != "" {
			return value
		}
	}

	return ""
}

func activityActorPtrFromRawMessage(data json.RawMessage) *ActivityActor {
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		return nil
	}

	return activityActorPtrFromObject(object)
}

func activityActorPtrFromObject(object map[string]any) *ActivityActor {
	actor := ActivityActor{}
	if nestedActor, ok := object["actor"].(map[string]any); ok {
		nestedProfileActor := activityActorFromProfileObject(nestedActor)
		mergeActivityActor(&actor, &nestedProfileActor)
	} else {
		actor = activityActorFromProfileObject(object)
	}
	if nestedActor, ok := object["miniProfile"].(map[string]any); ok {
		nestedProfileActor := activityActorFromProfileObject(nestedActor)
		mergeActivityActor(&actor, &nestedProfileActor)
	}
	if urn := stringField(object, "*actor", "actorUrn"); actor.URN == "" && urn != "" {
		actor.URN = urn
	}
	if !isActivityActorURN(actor.URN) {
		actor.URN = ""
	}
	if actor.DisplayName == "" {
		actor.DisplayName = actorNameFromObject(object)
	}
	if actor == (ActivityActor{}) {
		return nil
	}

	return &actor
}

func activityActorPtrFromConfirmedActorObject(object map[string]any) *ActivityActor {
	if nestedActor, ok := object["actor"].(map[string]any); ok {
		actor := activityActorFromProfileObject(nestedActor)
		if actor != (ActivityActor{}) {
			return &actor
		}
	}
	if nestedActor, ok := object["miniProfile"].(map[string]any); ok {
		actor := activityActorFromProfileObject(nestedActor)
		if actor != (ActivityActor{}) {
			return &actor
		}
	}
	if urn := stringField(object, "*actor", "actorUrn"); isActivityActorURN(urn) && urn != "" {
		actor := ActivityActor{URN: urn}
		return &actor
	}

	return nil
}

func activityActorFromProfileObject(object map[string]any) ActivityActor {
	actor := ActivityActor{
		URN:              stringField(object, jsonURNKey, "entityUrn"),
		PublicIdentifier: stringField(object, "publicIdentifier"),
		ProfileURL:       stringField(object, "profileUrl", "profileURL"),
		FirstName:        stringField(object, "firstName"),
		LastName:         stringField(object, "lastName"),
		DisplayName:      stringField(object, "displayName", "actorName", "name"),
		AvatarURL:        stringField(object, "avatarUrl", "avatarURL", "profilePictureUrl", "profilePicUrl"),
	}
	if name, ok := object["name"].(map[string]any); ok && actor.DisplayName == "" {
		actor.DisplayName = stringField(name, "text")
	}
	if actor.AvatarURL == "" {
		actor.AvatarURL = avatarURLFromProfilePicture(object["profilePicture"])
	}
	if actor.AvatarURL == "" {
		actor.AvatarURL = avatarURLFromProfilePicture(object["picture"])
	}
	return actor
}

func avatarURLFromProfilePicture(value any) string {
	profilePicture, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if url := stringField(profilePicture, "rootUrl", "url"); url != "" {
		return url
	}
	if displayImageReference, ok := profilePicture["displayImageReference"].(map[string]any); ok {
		if url := avatarURLFromProfilePicture(displayImageReference); url != "" {
			return url
		}
	}
	if vectorImage, ok := profilePicture["vectorImage"].(map[string]any); ok {
		return vectorImageURL(vectorImage)
	}

	return ""
}

func vectorImageURL(vectorImage map[string]any) string {
	rootURL := stringField(vectorImage, "rootUrl")
	artifacts, ok := vectorImage["artifacts"].([]any)
	if !ok {
		return rootURL
	}

	for i := range artifacts {
		artifact, ok := artifacts[i].(map[string]any)
		if !ok {
			continue
		}
		if fileIdentifyingURLPathSegment := stringField(artifact, "fileIdentifyingUrlPathSegment"); fileIdentifyingURLPathSegment != "" {
			return rootURL + fileIdentifyingURLPathSegment
		}
	}

	return rootURL
}

func mergeActivityActor(actor, secondary *ActivityActor) {
	if actor.URN == "" {
		actor.URN = secondary.URN
	}
	if actor.PublicIdentifier == "" {
		actor.PublicIdentifier = secondary.PublicIdentifier
	}
	if actor.ProfileURL == "" {
		actor.ProfileURL = secondary.ProfileURL
	}
	if actor.FirstName == "" {
		actor.FirstName = secondary.FirstName
	}
	if actor.LastName == "" {
		actor.LastName = secondary.LastName
	}
	if actor.DisplayName == "" {
		actor.DisplayName = secondary.DisplayName
	}
	if actor.AvatarURL == "" {
		actor.AvatarURL = secondary.AvatarURL
	}
}

func populateActivityActorCompatibility(item *ActivityItem) {
	if item.ActorURN == "" && item.Actor != nil {
		item.ActorURN = item.Actor.URN
	}
	if item.ActorName == "" && item.Actor != nil {
		item.ActorName = item.Actor.DisplayName
	}
	if item.CommentActorURN == "" && item.CommentActor != nil {
		item.CommentActorURN = item.CommentActor.URN
	}
	if item.CommentActorName == "" && item.CommentActor != nil {
		item.CommentActorName = item.CommentActor.DisplayName
	}
	if item.ReactionActorURN == "" && item.ReactionActor != nil {
		item.ReactionActorURN = item.ReactionActor.URN
	}
}

func isActivityActorURN(urn string) bool {
	return urn == "" ||
		strings.HasPrefix(urn, "urn:li:member:") ||
		strings.HasPrefix(urn, "urn:li:fs_profile:") ||
		strings.HasPrefix(urn, "urn:li:fsd_profile:") ||
		strings.HasPrefix(urn, "urn:li:organization:") ||
		strings.HasPrefix(urn, "urn:li:fsd_company:")
}

func actorURNFromObject(object map[string]any) string {
	if actor, ok := object["actor"].(map[string]any); ok {
		if urn := stringField(actor, "urn", "entityUrn"); urn != "" {
			return urn
		}
	}
	if urn := stringField(object, "*actor", "actorUrn"); urn != "" {
		return urn
	}

	return ""
}

func actorNameFromObject(object map[string]any) string {
	actor, ok := object["actor"].(map[string]any)
	if ok {
		if name := stringField(actor, "name", "displayName"); name != "" {
			return name
		}
		if name, ok := actor["name"].(map[string]any); ok {
			if value := stringField(name, "text"); value != "" {
				return value
			}
		}
	}
	if name := stringField(object, "actorName"); name != "" {
		return name
	}
	if name, ok := object["name"].(map[string]any); ok {
		if value := stringField(name, "text"); value != "" {
			return value
		}
	}

	return stringField(object, "name")
}

func textFromObject(object map[string]any) string {
	if text := stringField(object, "commentText"); text != "" {
		return text
	}
	for _, key := range []string{"message", "text", "body", "comment"} {
		if text := nestedTextFromObject(object, key); text != "" {
			return text
		}
	}
	if text := stringField(object, "message", "text", "body", "comment"); text != "" {
		return text
	}

	return ""
}

func nestedTextFromObject(object map[string]any, key string) string {
	textObject, ok := object[key].(map[string]any)
	if !ok {
		return ""
	}
	if nestedText, ok := textObject["text"].(map[string]any); ok {
		if text := stringField(nestedText, "text"); text != "" {
			return text
		}
	}

	return stringField(textObject, "text")
}

func graphQLCommentTextFromObject(object map[string]any) string {
	if commentary, ok := object["commentary"].(map[string]any); ok {
		if text, ok := commentary["text"].(map[string]any); ok {
			if value := stringField(text, "text"); value != "" {
				return value
			}
		}
		if value := stringField(commentary, "text", "plainText"); value != "" {
			return value
		}
	}
	if commentary, ok := object["commentaryV2"].(map[string]any); ok {
		if text, ok := commentary["text"].(map[string]any); ok {
			if value := stringField(text, "text"); value != "" {
				return value
			}
		}
		if value := stringField(commentary, "text"); value != "" {
			return value
		}
		if attributedText, ok := commentary["attributedText"].(map[string]any); ok {
			if value := stringField(attributedText, "text"); value != "" {
				return value
			}
		}
		if value := stringField(commentary, "attributedText", "plainText"); value != "" {
			return value
		}
	}

	return textFromObject(object)
}

func graphQLCommentCreatedAtFromObject(object map[string]any) time.Time {
	if createdAt := timestampMillisFromObject(object, "createdAt", "publishedAt"); createdAt > 0 {
		return time.Unix(createdAt/1000, 0)
	}
	if created, ok := object["created"].(map[string]any); ok {
		if createdAt := timestampMillisFromObject(created, "time"); createdAt > 0 {
			return time.Unix(createdAt/1000, 0)
		}
	}
	if createdAt := timestampMillisFromObject(object, "createdAtTimestamp"); createdAt > 0 {
		return time.Unix(createdAt/1000, 0)
	}

	return time.Time{}
}

func timestampMillisFromObject(object map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch value := object[key].(type) {
		case float64:
			if value > 0 {
				return int64(value)
			}
		case int64:
			if value > 0 {
				return value
			}
		case int:
			if value > 0 {
				return int64(value)
			}
		}
	}

	return 0
}

func isContentSignalPath(pathText string) bool {
	if strings.Contains(pathText, "actor") || strings.Contains(pathText, "miniprofile") || strings.Contains(pathText, "profilepicture") {
		return false
	}

	return strings.Contains(pathText, "content") ||
		strings.Contains(pathText, "media") ||
		strings.Contains(pathText, "attachment") ||
		strings.Contains(pathText, "article") ||
		strings.Contains(pathText, "carousel") ||
		strings.Contains(pathText, "image") ||
		strings.Contains(pathText, "video") ||
		strings.Contains(pathText, "document") ||
		strings.Contains(pathText, "event")
}

func isActivityType(typeName string) bool {
	typeText := strings.ToLower(typeName)
	return strings.Contains(typeName, "Update") || strings.Contains(typeName, "Activity") || strings.Contains(typeName, "Share") || isCommentType(typeText)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}

	return 0
}

func activityURLFromURN(urn string) string {
	urn = normalizeActivityURN(urn)
	if urn == "" {
		return ""
	}

	const prefix = "urn:li:activity:"
	if !strings.HasPrefix(urn, prefix) {
		return ""
	}

	return fmt.Sprintf("https://www.linkedin.com/feed/update/%s", urn)
}

func feedUpdateURLFromURN(urn string) string {
	if strings.HasPrefix(urn, "urn:li:activity:") {
		return activityURLFromURN(urn)
	}
	if strings.HasPrefix(urn, "urn:li:ugcPost:") {
		return fmt.Sprintf("https://www.linkedin.com/feed/update/%s", urn)
	}

	return ""
}

func commentActivityURL(parentURN, commentURN string) string {
	parentURN = normalizeCommentParentURN(parentURN)
	if parentURN == "" || commentedOnURNFromCommentURN(commentURN) != parentURN {
		return ""
	}

	commentID := commentIDFromURN(commentURN)
	if commentID == "" {
		return ""
	}

	dashCommentURN := fmt.Sprintf("urn:li:fsd_comment:(%s,%s)", commentID, parentURN)
	query := url.Values{}
	query.Set("commentUrn", commentURN)
	query.Set("dashCommentUrn", dashCommentURN)

	return feedUpdateURLFromURN(parentURN) + "?" + query.Encode()
}

func normalizeActivityURN(urn string) string {
	return activityURNPattern.FindString(urn)
}

func nonEmptyUniqueStrings(values ...string) []string {
	unique := make([]string, 0, len(values))
	return appendUniqueStrings(unique, values...)
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		if addition == "" || stringSliceContains(values, addition) {
			continue
		}
		values = append(values, addition)
	}

	return values
}

func stringSliceContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}

	return false
}

// parseFeedItem parses a single feed item.
func parseFeedItem(data json.RawMessage) (*FeedItem, error) {
	var entity struct {
		EntityURN string `json:"entityUrn"`
		Actor     struct {
			URN  string `json:"urn"`
			Name struct {
				Text string `json:"text"`
			} `json:"name"`
		} `json:"actor"`
		Commentary struct {
			Text struct {
				Text string `json:"text"`
			} `json:"text"`
		} `json:"commentary"`
		SocialDetail struct {
			LikesCount    int `json:"likes,omitempty"`
			CommentsCount int `json:"comments,omitempty"`
		} `json:"socialDetail"`
		CreatedAt   int64  `json:"createdAt"`
		PublishedAt int64  `json:"publishedAt"`
		ActorURN    string `json:"*actor"`
	}

	if err := json.Unmarshal(data, &entity); err != nil {
		return nil, err
	}

	if entity.EntityURN == "" {
		return nil, fmt.Errorf("no URN in feed item")
	}

	item := &FeedItem{
		URN:  entity.EntityURN,
		Type: "update",
	}
	if entity.CreatedAt > 0 {
		item.CreatedAt = time.Unix(entity.CreatedAt/1000, 0)
	} else if entity.PublishedAt > 0 {
		item.CreatedAt = time.Unix(entity.PublishedAt/1000, 0)
	}

	if entity.Commentary.Text.Text != "" {
		item.Post = &Post{
			URN:          entity.EntityURN,
			AuthorURN:    entity.Actor.URN,
			Text:         entity.Commentary.Text.Text,
			CreatedAt:    item.CreatedAt,
			LikeCount:    entity.SocialDetail.LikesCount,
			CommentCount: entity.SocialDetail.CommentsCount,
		}
		if item.Post.AuthorURN == "" {
			item.Post.AuthorURN = entity.ActorURN
		}
	}

	if entity.Actor.Name.Text != "" {
		item.Actor = &Profile{
			URN:       entity.Actor.URN,
			FirstName: entity.Actor.Name.Text,
		}
		if item.Post != nil {
			item.Post.AuthorName = entity.Actor.Name.Text
		}
	}

	return item, nil
}

// CreatePost creates a new LinkedIn post.
func (c *Client) CreatePost(ctx context.Context, text string) (*Post, error) {
	// Use the Voyager content creation endpoint.
	payload := map[string]any{
		"visibleToConnectionsOnly":  false,
		"externalAudienceProviders": []any{},
		"commentaryV2": map[string]any{
			"text":       text,
			"attributes": []any{},
		},
		"origin":                 "FEED",
		"allowedCommentersScope": "ALL",
		"postState":              "PUBLISHED",
	}

	var result struct {
		Data struct {
			Status struct {
				URN      string `json:"urn"`
				UpdateV2 string `json:"*updateV2"`
			} `json:"status"`
		} `json:"data"`
	}

	if err := c.Post(ctx, "/contentcreation/normShares", payload, &result); err != nil {
		return nil, err
	}

	return &Post{
		URN:  result.Data.Status.URN,
		Text: text,
	}, nil
}

// DeletePost deletes a post by URN.
func (c *Client) DeletePost(ctx context.Context, urn string) error {
	// URL encode the URN.
	encodedURN := url.PathEscape(urn)
	return c.Delete(ctx, "/contentcreation/normShares/"+encodedURN)
}

// GetPost fetches a post by URN.
func (c *Client) GetPost(ctx context.Context, urn string) (*Post, error) {
	// URL encode the URN.
	encodedURN := url.PathEscape(urn)

	var result VoyagerResponse
	if err := c.Get(ctx, "/feed/updates/"+encodedURN, nil, &result); err != nil {
		return nil, err
	}

	// Parse the post from response.
	for _, raw := range result.Included {
		item, err := parseFeedItem(raw)
		if err == nil && item != nil && item.Post != nil {
			return item.Post, nil
		}
	}

	return nil, &Error{
		Code:    ErrCodeNotFound,
		Message: "post not found",
	}
}

// SearchOptions configures search parameters.
type SearchOptions struct {
	Limit int
	Start int
}

// searchResult is the common response structure for GraphQL search queries.
type searchResult struct {
	Data     json.RawMessage   `json:"data"`
	Included []json.RawMessage `json:"included"`
}

// buildSearchPath constructs the GraphQL search path for a given result type.
func buildSearchPath(query, resultType string, start int) string {
	encodedQuery := url.QueryEscape(query)
	return fmt.Sprintf(
		"/graphql?variables=(start:%d,origin:GLOBAL_SEARCH_HEADER,query:(keywords:%s,flagshipSearchIntent:SEARCH_SRP,queryParameters:List((key:resultType,value:List(%s))),includeFiltersInResponse:false))&queryId=voyagerSearchDashClusters.b0928897b71bd00a5a7291755dcd64f0",
		start,
		encodedQuery,
		resultType,
	)
}

// SearchPeople searches for people on LinkedIn.
func (c *Client) SearchPeople(ctx context.Context, query string, opts *SearchOptions) ([]Profile, error) {
	if opts == nil {
		opts = &SearchOptions{Limit: 10}
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	var result searchResult
	if err := c.Get(ctx, buildSearchPath(query, "PEOPLE", opts.Start), nil, &result); err != nil {
		return nil, err
	}

	return parseSearchPeopleResults(result.Included)
}

// parseSearchPeopleResults extracts profiles from search results.
func parseSearchPeopleResults(included []json.RawMessage) ([]Profile, error) {
	profiles := make([]Profile, 0, len(included))

	for _, raw := range included {
		var entity struct {
			Type  string `json:"$type"`
			Title *struct {
				Text string `json:"text"`
			} `json:"title"`
			PrimarySubtitle *struct {
				Text string `json:"text"`
			} `json:"primarySubtitle"`
			SecondarySubtitle *struct {
				Text string `json:"text"`
			} `json:"secondarySubtitle"`
			NavigationURL string `json:"navigationUrl"`
			TrackingURN   string `json:"trackingUrn"`
			BadgeText     *struct {
				Text string `json:"text"`
			} `json:"badgeText"`
		}

		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}

		// Only process EntityResultViewModel for people.
		if entity.Type != "com.linkedin.voyager.dash.search.EntityResultViewModel" {
			continue
		}

		// Check if it's a person (trackingUrn contains "member").
		if !strings.Contains(entity.TrackingURN, "member") {
			continue
		}

		profile := Profile{
			URN:        entity.TrackingURN,
			ProfileURL: entity.NavigationURL,
		}

		if entity.Title != nil {
			// Parse first and last name from title.
			parts := strings.SplitN(entity.Title.Text, " ", 2)
			if len(parts) >= 1 {
				profile.FirstName = parts[0]
			}
			if len(parts) >= 2 {
				profile.LastName = parts[1]
			}
		}

		if entity.PrimarySubtitle != nil {
			profile.Headline = entity.PrimarySubtitle.Text
		}

		if entity.SecondarySubtitle != nil {
			profile.Location = entity.SecondarySubtitle.Text
		}

		// Extract public ID from URL.
		if entity.NavigationURL != "" {
			if idx := strings.Index(entity.NavigationURL, "/in/"); idx != -1 {
				publicID := entity.NavigationURL[idx+4:]
				if qIdx := strings.Index(publicID, "?"); qIdx != -1 {
					publicID = publicID[:qIdx]
				}
				profile.PublicID = publicID
			}
		}

		profiles = append(profiles, profile)
	}

	return profiles, nil
}

// SearchCompanies searches for companies on LinkedIn.
func (c *Client) SearchCompanies(ctx context.Context, query string, opts *SearchOptions) ([]Company, error) {
	if opts == nil {
		opts = &SearchOptions{Limit: 10}
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	var result searchResult
	if err := c.Get(ctx, buildSearchPath(query, "COMPANIES", opts.Start), nil, &result); err != nil {
		return nil, err
	}

	return parseSearchCompanyResults(result.Included)
}

// parseSearchCompanyResults extracts companies from search results.
func parseSearchCompanyResults(included []json.RawMessage) ([]Company, error) {
	companies := make([]Company, 0, len(included))

	for _, raw := range included {
		var entity struct {
			Type  string `json:"$type"`
			Title *struct {
				Text string `json:"text"`
			} `json:"title"`
			PrimarySubtitle *struct {
				Text string `json:"text"`
			} `json:"primarySubtitle"`
			SecondarySubtitle *struct {
				Text string `json:"text"`
			} `json:"secondarySubtitle"`
			Summary *struct {
				Text string `json:"text"`
			} `json:"summary"`
			NavigationURL string `json:"navigationUrl"`
			TrackingURN   string `json:"trackingUrn"`
		}

		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}

		// Only process EntityResultViewModel for companies.
		if entity.Type != "com.linkedin.voyager.dash.search.EntityResultViewModel" {
			continue
		}

		// Check if it's a company (trackingUrn contains "company").
		if !strings.Contains(entity.TrackingURN, "company") {
			continue
		}

		company := Company{
			URN:        entity.TrackingURN,
			CompanyURL: entity.NavigationURL,
		}

		if entity.Title != nil {
			company.Name = entity.Title.Text
		}

		if entity.PrimarySubtitle != nil {
			// Primary subtitle contains "Industry • Location".
			parts := strings.SplitN(entity.PrimarySubtitle.Text, " • ", 2)
			if len(parts) >= 1 {
				company.Industry = parts[0]
			}
			if len(parts) >= 2 {
				company.Location = parts[1]
			}
		}

		if entity.SecondarySubtitle != nil {
			company.FollowerCount = entity.SecondarySubtitle.Text
		}

		if entity.Summary != nil {
			company.Description = entity.Summary.Text
		}

		companies = append(companies, company)
	}

	return companies, nil
}

// MessagingOptions configures messaging requests.
type MessagingOptions struct {
	Limit int
	Start int
}

// GetConversations fetches the user's messaging conversations.
func (c *Client) GetConversations(ctx context.Context, opts *MessagingOptions) ([]Conversation, error) {
	if opts == nil {
		opts = &MessagingOptions{Limit: 20}
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}

	// Try multiple endpoint strategies as LinkedIn changes their API frequently.
	endpoints := []struct {
		path  string
		query url.Values
	}{
		// Strategy 1: New dash messaging with GraphQL decoration
		{
			path: "/voyagerMessagingDashConversations",
			query: url.Values{
				"decorationId": {"com.linkedin.voyager.dash.deco.messaging.FullConversation-46"},
				"count":        {fmt.Sprintf("%d", opts.Limit)},
				"q":            {"syncToken"},
			},
		},
		// Strategy 2: Messaging GraphQL
		{
			path: "/voyagerMessagingGraphQL/graphql",
			query: url.Values{
				"queryId":   {"messengerConversations.b82e44e85e0e8d228d5bb0e67d1c5c79"},
				"variables": {fmt.Sprintf("(count:%d)", opts.Limit)},
			},
		},
		// Strategy 3: Legacy messaging API
		{
			path: "/messaging/conversations",
			query: url.Values{
				"keyVersion": {"LEGACY_INBOX"},
			},
		},
		// Strategy 4: Dash messaging threads
		{
			path: "/voyagerMessagingDashMessagingThreads",
			query: url.Values{
				"decorationId": {"com.linkedin.voyager.dash.deco.messaging.Thread-7"},
				"count":        {fmt.Sprintf("%d", opts.Limit)},
				"q":            {"inboxThreads"},
			},
		},
	}

	var lastErr error
	for _, ep := range endpoints {
		var result VoyagerResponse
		if err := c.Get(ctx, ep.path, ep.query, &result); err != nil {
			lastErr = err
			continue
		}

		// Check if we got a valid response with data.
		if len(result.Included) > 0 {
			conversations, err := parseConversationsFromResponse(&result)
			if err == nil && len(conversations) > 0 {
				return conversations, nil
			}
		}
	}

	if lastErr != nil {
		if strings.Contains(lastErr.Error(), "status 500") || strings.Contains(lastErr.Error(), "status 400") {
			return nil, &Error{
				Code:    ErrCodeServerError,
				Message: "LinkedIn messaging API is currently unavailable. LinkedIn frequently changes their internal API. Try using LinkedIn's web interface instead.",
			}
		}
		return nil, lastErr
	}

	return []Conversation{}, nil
}

// extractProfilesFromIncluded builds a map of profiles from Voyager included data.
func extractProfilesFromIncluded(included []json.RawMessage) map[string]*Profile {
	profiles := make(map[string]*Profile)
	for _, raw := range included {
		var entity struct {
			Type             string `json:"$type"`
			EntityURN        string `json:"entityUrn"`
			FirstName        string `json:"firstName"`
			LastName         string `json:"lastName"`
			Occupation       string `json:"occupation"`
			PublicIdentifier string `json:"publicIdentifier"`
		}
		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}
		if strings.Contains(entity.Type, "MiniProfile") || strings.Contains(entity.Type, "Profile") {
			if entity.EntityURN != "" {
				profiles[entity.EntityURN] = &Profile{
					URN:       entity.EntityURN,
					FirstName: entity.FirstName,
					LastName:  entity.LastName,
					Headline:  entity.Occupation,
					PublicID:  entity.PublicIdentifier,
				}
			}
		}
	}
	return profiles
}

// parseConversationsFromResponse extracts conversations from a Voyager response.
func parseConversationsFromResponse(resp *VoyagerResponse) ([]Conversation, error) {
	if resp == nil {
		return nil, &Error{
			Code:    ErrCodeServerError,
			Message: "empty response",
		}
	}

	profiles := extractProfilesFromIncluded(resp.Included)

	conversations := make([]Conversation, 0, len(resp.Included))
	for _, raw := range resp.Included {
		var entity struct {
			Type            string   `json:"$type"`
			EntityURN       string   `json:"entityUrn"`
			Read            bool     `json:"read"`
			LastActivityAt  int64    `json:"lastActivityAt"`
			TotalEventCount int      `json:"totalEventCount"`
			Participants    []string `json:"*participants"`
			Events          []string `json:"*events"`
		}
		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}

		if !strings.Contains(entity.Type, "Conversation") {
			continue
		}

		conv := Conversation{
			URN:         entity.EntityURN,
			Unread:      !entity.Read,
			TotalEvents: entity.TotalEventCount,
		}

		if entity.LastActivityAt > 0 {
			conv.LastActivityAt = time.Unix(entity.LastActivityAt/1000, 0)
		}

		// Resolve participant profiles.
		for _, pURN := range entity.Participants {
			if p, ok := profiles[pURN]; ok {
				conv.Participants = append(conv.Participants, *p)
			}
		}

		conversations = append(conversations, conv)
	}

	return conversations, nil
}

// GetConversation fetches a specific conversation with messages.
func (c *Client) GetConversation(ctx context.Context, conversationURN string) (*Conversation, []Message, error) {
	// URL encode the URN.
	encodedURN := url.PathEscape(conversationURN)

	query := url.Values{}
	query.Set("keyVersion", "LEGACY_INBOX")

	var result VoyagerResponse
	if err := c.Get(ctx, "/messaging/conversations/"+encodedURN+"/events", query, &result); err != nil {
		return nil, nil, err
	}

	return parseConversationWithMessages(&result, conversationURN)
}

// parseConversationWithMessages extracts a conversation and its messages.
func parseConversationWithMessages(resp *VoyagerResponse, conversationURN string) (*Conversation, []Message, error) {
	if resp == nil {
		return nil, nil, &Error{
			Code:    ErrCodeServerError,
			Message: "empty response",
		}
	}

	profiles := extractProfilesFromIncluded(resp.Included)

	conv := &Conversation{URN: conversationURN}
	messages := make([]Message, 0, len(resp.Included))

	for _, raw := range resp.Included {
		var entity struct {
			Type         string `json:"$type"`
			EntityURN    string `json:"entityUrn"`
			CreatedAt    int64  `json:"createdAt"`
			From         string `json:"*from"`
			EventContent struct {
				Type           string `json:"$type"`
				AttributedBody struct {
					Text string `json:"text"`
				} `json:"attributedBody"`
			} `json:"eventContent"`
		}
		if err := json.Unmarshal(raw, &entity); err != nil {
			continue
		}

		if !strings.Contains(entity.Type, "Event") {
			continue
		}

		// Only process message events.
		if !strings.Contains(entity.EventContent.Type, "MessageEvent") {
			continue
		}

		msg := Message{
			URN:       entity.EntityURN,
			SenderURN: entity.From,
			Text:      entity.EventContent.AttributedBody.Text,
		}

		if entity.CreatedAt > 0 {
			msg.CreatedAt = time.Unix(entity.CreatedAt/1000, 0)
		}

		// Get sender name.
		if p, ok := profiles[entity.From]; ok {
			msg.SenderName = p.FirstName + " " + p.LastName
		}

		messages = append(messages, msg)
	}

	// Sort messages by creation time (oldest first).
	for i := 0; i < len(messages)-1; i++ {
		for j := i + 1; j < len(messages); j++ {
			if messages[i].CreatedAt.After(messages[j].CreatedAt) {
				messages[i], messages[j] = messages[j], messages[i]
			}
		}
	}

	return conv, messages, nil
}

// SendMessage sends a message to a profile.
func (c *Client) SendMessage(ctx context.Context, profileURN, text string) (*Message, error) {
	// First, we need to get or create a conversation with this profile.
	// LinkedIn requires creating a conversation first or using an existing one.

	// Get the current user's profile URN.
	myProfile, err := c.GetMyProfile(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get own profile: %w", err)
	}

	// Create the message payload.
	payload := map[string]any{
		"keyVersion": "LEGACY_INBOX",
		"conversationCreate": map[string]any{
			"recipients": []string{profileURN},
			"subtype":    "MEMBER_TO_MEMBER",
		},
		"message": map[string]any{
			"body": map[string]any{
				"text": text,
			},
		},
	}

	var result map[string]any
	if err := c.Post(ctx, "/messaging/conversations", payload, &result); err != nil {
		return nil, err
	}

	return &Message{
		SenderURN: myProfile.URN,
		Text:      text,
		CreatedAt: time.Now(),
	}, nil
}

// SendMessageToConversation sends a message to an existing conversation.
func (c *Client) SendMessageToConversation(ctx context.Context, conversationURN, text string) (*Message, error) {
	// URL encode the URN.
	encodedURN := url.PathEscape(conversationURN)

	payload := map[string]any{
		"keyVersion": "LEGACY_INBOX",
		"eventCreate": map[string]any{
			"value": map[string]any{
				"com.linkedin.voyager.messaging.create.MessageCreate": map[string]any{
					"body":        text,
					"attachments": []any{},
				},
			},
		},
	}

	var result map[string]any
	if err := c.Post(ctx, "/messaging/conversations/"+encodedURN+"/events", payload, &result); err != nil {
		return nil, err
	}

	return &Message{
		Text:      text,
		CreatedAt: time.Now(),
	}, nil
}
