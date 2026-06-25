// Package api provides the LinkedIn Voyager API client.
package api

import "time"

// Response wraps all API responses with success status.
type Response[T any] struct {
	Success bool   `json:"success"`
	Data    T      `json:"data,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error represents an API error response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Common error codes.
const (
	ErrCodeAuthExpired  = "AUTH_EXPIRED"
	ErrCodeAuthRequired = "AUTH_REQUIRED"
	ErrCodeRateLimited  = "RATE_LIMITED"
	ErrCodeNotFound     = "NOT_FOUND"
	ErrCodeForbidden    = "FORBIDDEN"
	ErrCodeServerError  = "SERVER_ERROR"
	ErrCodeNetworkError = "NETWORK_ERROR"
	ErrCodeInvalidInput = "INVALID_INPUT"
)

// Credentials holds LinkedIn authentication cookies.
type Credentials struct {
	LiAt      string    `json:"li_at"`
	JSessID   string    `json:"jsessionid"`
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// IsValid checks if credentials are present and not expired.
func (c *Credentials) IsValid() bool {
	if c.LiAt == "" || c.JSessID == "" {
		return false
	}
	if !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt) {
		return false
	}
	return true
}

// Profile represents a LinkedIn user profile.
type Profile struct {
	URN           string `json:"urn"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	Headline      string `json:"headline,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Location      string `json:"location,omitempty"`
	ProfileURL    string `json:"profileUrl,omitempty"`
	ProfilePicURL string `json:"profilePicUrl,omitempty"`
	PublicID      string `json:"publicId,omitempty"`
}

// Post represents a LinkedIn post.
type Post struct {
	URN          string    `json:"urn"`
	AuthorURN    string    `json:"authorUrn"`
	AuthorName   string    `json:"authorName,omitempty"`
	Text         string    `json:"text"`
	CreatedAt    time.Time `json:"createdAt"`
	LikeCount    int       `json:"likeCount"`
	CommentCount int       `json:"commentCount"`
	ShareCount   int       `json:"shareCount"`
}

// FeedItem represents an item in the LinkedIn feed.
type FeedItem struct {
	URN       string    `json:"urn"`
	Type      string    `json:"type"`
	Post      *Post     `json:"post,omitempty"`
	Actor     *Profile  `json:"actor,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// RecentActivityOptions configures recent profile activity fetching.
type RecentActivityOptions struct {
	Limit    int
	Start    int
	Category RecentActivityCategory
}

// RecentActivityCategory identifies a LinkedIn recent activity category.
type RecentActivityCategory string

// Supported recent activity categories.
const (
	RecentActivityCategoryAll       RecentActivityCategory = "all"
	RecentActivityCategoryImages    RecentActivityCategory = "images"
	RecentActivityCategoryVideos    RecentActivityCategory = "videos"
	RecentActivityCategoryDocuments RecentActivityCategory = "documents"
	RecentActivityCategoryEvents    RecentActivityCategory = "events"
	RecentActivityCategoryReactions RecentActivityCategory = "reactions"
)

// ActivityItem represents a recent LinkedIn profile activity item.
type ActivityItem struct {
	URN             string                 `json:"urn"`
	Type            string                 `json:"type"`
	ActorURN        string                 `json:"actorUrn,omitempty"`
	ActorName       string                 `json:"actorName,omitempty"`
	Text            string                 `json:"text,omitempty"`
	CreatedAt       time.Time              `json:"createdAt,omitzero"`
	LikeCount       int                    `json:"likeCount,omitempty"`
	CommentCount    int                    `json:"commentCount,omitempty"`
	ShareCount      int                    `json:"shareCount,omitempty"`
	URL             string                 `json:"url,omitempty"`
	RawURN          string                 `json:"rawUrn,omitempty"`
	ContentCategory RecentActivityCategory `json:"contentCategory,omitempty"`
}

// Conversation represents a LinkedIn messaging conversation.
type Conversation struct {
	URN            string    `json:"urn"`
	Participants   []Profile `json:"participants"`
	LastMessage    *Message  `json:"lastMessage,omitempty"`
	LastActivityAt time.Time `json:"lastActivityAt"`
	Unread         bool      `json:"unread"`
	TotalEvents    int       `json:"totalEvents,omitempty"`
}

// Message represents a LinkedIn message.
type Message struct {
	URN        string    `json:"urn"`
	SenderURN  string    `json:"senderUrn"`
	SenderName string    `json:"senderName,omitempty"`
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"createdAt"`
}

// SearchResult represents a search result item.
type SearchResult struct {
	URN     string   `json:"urn"`
	Type    string   `json:"type"`
	Profile *Profile `json:"profile,omitempty"`
	Company *Company `json:"company,omitempty"`
	Job     *Job     `json:"job,omitempty"`
}

// Company represents a LinkedIn company.
type Company struct {
	URN           string `json:"urn"`
	Name          string `json:"name"`
	Industry      string `json:"industry,omitempty"`
	Description   string `json:"description,omitempty"`
	Website       string `json:"website,omitempty"`
	LogoURL       string `json:"logoUrl,omitempty"`
	EmployeeCount string `json:"employeeCount,omitempty"`
	Location      string `json:"location,omitempty"`
	FollowerCount string `json:"followerCount,omitempty"`
	CompanyURL    string `json:"companyUrl,omitempty"`
}

// Job represents a LinkedIn job posting.
type Job struct {
	URN         string    `json:"urn"`
	Title       string    `json:"title"`
	CompanyName string    `json:"companyName"`
	Location    string    `json:"location,omitempty"`
	PostedAt    time.Time `json:"postedAt,omitempty"`
	Description string    `json:"description,omitempty"`
}
