package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

const defaultActivityLimit = 10

type recentActivityEndpoint struct {
	path  string
	query url.Values
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
		return RecentActivityOptions{Limit: defaultActivityLimit}
	}
	if opts.Limit <= 0 {
		opts.Limit = defaultActivityLimit
	}
	return *opts
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

	endpoints := buildRecentActivityEndpoints(username, profile.URN, activityOptions)
	var lastErr error
	for _, endpoint := range endpoints {
		var result VoyagerResponse
		if err := c.Get(ctx, endpoint.path, endpoint.query, &result); err != nil {
			if isTerminalActivityError(err) {
				return nil, err
			}
			lastErr = err
			continue
		}

		items, err := parseRecentActivityFromResponse(&result)
		if err != nil {
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
	count := fmt.Sprintf("%d", opts.Limit)
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
		},
	}
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

	items := make([]ActivityItem, 0)
	candidateCount := 0
	parseErrors := make([]string, 0)
	for _, raw := range appendActivityElements(resp.Data, resp.Included) {
		if !isActivityCandidate(raw) {
			continue
		}

		candidateCount++
		item, err := parseActivityItem(raw)
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

func isActivityCandidate(data json.RawMessage) bool {
	var entity struct {
		Type string `json:"$type"`
	}
	if err := json.Unmarshal(data, &entity); err != nil {
		return false
	}

	return isActivityType(entity.Type)
}

// parseProfileActivityFromResponse extracts profile activity as feed-compatible items.
func parseProfileActivityFromResponse(resp *VoyagerResponse) ([]FeedItem, error) {
	items, err := parseRecentActivityFromResponse(resp)
	if err != nil {
		return nil, err
	}

	return activityItemsToFeedItems(items), nil
}

func appendActivityElements(data json.RawMessage, included []json.RawMessage) []json.RawMessage {
	elements := make([]json.RawMessage, 0, len(included)+1)

	if len(data) > 0 {
		var dataResp struct {
			Elements []json.RawMessage `json:"elements"`
		}
		if err := json.Unmarshal(data, &dataResp); err == nil && len(dataResp.Elements) > 0 {
			elements = append(elements, dataResp.Elements...)
		} else {
			elements = append(elements, data)
		}
	}

	elements = append(elements, included...)
	return elements
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

func parseActivityItem(data json.RawMessage) (*ActivityItem, error) {
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

	urn := firstNonEmpty(entity.EntityURN, entity.URN)
	if urn == "" {
		return nil, fmt.Errorf("no URN in activity item")
	}

	item := &ActivityItem{
		URN:          urn,
		Type:         entity.Type,
		ActorURN:     firstNonEmpty(entity.Actor.URN, entity.ActorURN),
		ActorName:    entity.Actor.Name.Text,
		Text:         firstNonEmpty(entity.Commentary.Text.Text, entity.CommentaryV2.Text, entity.Text.Text),
		LikeCount:    firstNonZero(entity.SocialDetail.LikesCount, entity.SocialActivityCounts.NumLikes),
		CommentCount: firstNonZero(entity.SocialDetail.CommentsCount, entity.SocialActivityCounts.NumComments),
		ShareCount:   firstNonZero(entity.SocialDetail.SharesCount, entity.SocialActivityCounts.NumShares),
		URL:          firstNonEmpty(entity.URL, activityURLFromURN(urn)),
		RawURN:       urn,
	}
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

func isActivityType(typeName string) bool {
	return strings.Contains(typeName, "Update") || strings.Contains(typeName, "Activity") || strings.Contains(typeName, "Share")
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
	const prefix = "urn:li:activity:"
	if !strings.HasPrefix(urn, prefix) {
		return ""
	}

	return fmt.Sprintf("https://www.linkedin.com/feed/update/%s", urn)
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
