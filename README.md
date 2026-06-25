# lnk

> A fast LinkedIn CLI for posting, reading, searching, and messaging via LinkedIn's Voyager API.

Inspired by [bird](https://github.com/steipete/bird) for X/Twitter.

## Features

- **Posts**: Create, read, and delete posts
- **Profiles**: View profiles by username or URN
- **Search**: Search for people and companies
- **Messaging**: View conversations and send messages
- **Agent-Friendly**: JSON output mode for AI agent integration
- **Cross-Platform**: Works on macOS and Linux

## Installation

### From Source

```bash
git clone https://github.com/bvgroup-co/lnk.git
cd lnk
go build -o lnk ./cmd/lnk
```

### Using Go Install

```bash
go install github.com/bvgroup-co/lnk/cmd/lnk@latest
```

## Quick Start

```bash
# 1. Authenticate with email/password
lnk auth login -e your@email.com

# 2. Or authenticate with browser cookies
lnk auth login --browser safari

# 3. Check auth status
lnk auth status

# 4. View your profile
lnk profile me

# 5. Create a post
lnk post create "Hello LinkedIn!"

# 6. Search for people
lnk search people "software engineer"
```

## Authentication

### Email/Password (Recommended)

```bash
lnk auth login -e your@email.com
# You'll be prompted for your password securely
```

Or provide password directly:
```bash
lnk auth login -e your@email.com -p "yourpassword"
```

### Browser Cookies

```bash
lnk auth login --browser safari   # macOS only
lnk auth login --browser chrome   # macOS/Linux
lnk auth login --browser firefox  # macOS/Linux
lnk auth login --browser brave    # macOS/Linux
lnk auth login --browser arc      # macOS
```

**Note**: May require granting Full Disk Access to your terminal application in System Preferences > Privacy & Security.

### Direct Cookie Input

```bash
lnk auth login --li-at "your-li_at-cookie" --jsessionid "your-jsessionid-cookie"
```

### Environment Variables

```bash
export LNK_LI_AT="your-li_at-cookie"
export LNK_JSESSIONID="your-jsessionid-cookie"
lnk auth login --env
```

## Commands Reference

### Authentication

| Command | Description |
|---------|-------------|
| `lnk auth login -e <email>` | Authenticate with email/password |
| `lnk auth login --browser <name>` | Authenticate using browser cookies |
| `lnk auth status` | Check authentication status |
| `lnk auth logout` | Clear stored credentials |

### Profiles

| Command | Description |
|---------|-------------|
| `lnk profile me` | View your own profile |
| `lnk profile get <username>` | View a profile by username |
| `lnk profile get --urn <urn>` | View a profile by URN |
| `lnk profile activity <username>` | View recent profile activity |
| `lnk profile activity <username> --category posts` | View profile posts via Web UI GraphQL shape |
| `lnk profile activity <username> --category comments` | View profile comments via Web UI GraphQL shape |
| `lnk profile activity <username> --category reactions` | View profile reactions via Web UI GraphQL shape |
| `lnk profile activity <username> --category images` | View image activity |
| `lnk profile activity <username> --limit 20` | Limit recent activity items |

### Posts

| Command | Description |
|---------|-------------|
| `lnk post create <text>` | Create a new post |
| `lnk post create --file post.txt` | Create post from file |
| `lnk post get <urn>` | Read a post by URN |
| `lnk post delete <urn>` | Delete a post by URN |

### Search

| Command | Description |
|---------|-------------|
| `lnk search people <query>` | Search for people |
| `lnk search companies <query>` | Search for companies |
| `lnk search people <query> --limit 20` | Limit results |

### Messaging

| Command | Description |
|---------|-------------|
| `lnk messages list` | List conversations |
| `lnk messages get <conversation-urn>` | View messages in a conversation |
| `lnk messages send <username> <text>` | Send a message to a user |
| `lnk messages reply <conversation-urn> <text>` | Reply to a conversation |

**Aliases**: `msg`, `dm` (e.g., `lnk msg list`)

### Feed

| Command | Description |
|---------|-------------|
| `lnk feed` | Read your feed |
| `lnk feed --limit 20` | Read more feed items |

### Recent Profile Activity

```bash
lnk profile activity johndoe --category all
lnk profile activity johndoe --category posts
lnk profile activity johndoe --category comments
lnk profile activity johndoe --category reactions --json
lnk profile activity johndoe --category posts --experimental-local-filter
lnk profile activity johndoe --category comments --experimental-local-filter
lnk profile activity johndoe --category reactions --experimental-local-filter --json
lnk profile activity johndoe --category images --experimental-local-filter --json
lnk profile activity johndoe --category videos --experimental-local-filter --limit 20
lnk profile activity johndoe --category documents --experimental-local-filter
lnk profile activity johndoe --category events --experimental-local-filter
lnk profile activity johndoe --category all --debug-shape --json
lnk profile activity johndoe --limit 20
lnk profile activity johndoe --json
```

The default `all` category fetches LinkedIn's generic authenticated Voyager
activity feed using `/feed/updatesV2?q=memberShareFeed&profileUrn=...`, with a
legacy `/feed/updates?profileId=...&q=memberShareFeed&moduleKey=member-share`
fallback. It preserves historical behavior and is not guaranteed to match the
LinkedIn Web UI `all` tab.

The `posts`, `comments`, and `reactions` categories are supported without
experimental flags and use the Web UI GraphQL profile updates shapes:

```sh
lnk profile activity johndoe --category posts --json
lnk profile activity johndoe --category comments --json
lnk profile activity johndoe --category reactions --json
```

They call `/voyager/api/graphql` with category-specific
`voyagerFeedDashProfileUpdates` query IDs. The normalized response parser reads
the category collection `*elements` and resolves those references from
`included`. Comment and reaction detail fields are returned only when the
response structure contains them; otherwise output stays limited to stable
activity/update fields.

Category compatibility is under active work:

| Category | Status | Notes |
|---|---|---|
| `all` | Supported generic Voyager feed | Historical behavior; not guaranteed Web UI-equivalent. |
| `posts` | Supported via Web UI GraphQL | `feedDashProfileUpdatesByMemberShareFeed`. |
| `comments` | Supported via Web UI GraphQL | `feedDashProfileUpdatesByMemberComments`. |
| `reactions` | Supported via Web UI GraphQL | `feedDashProfileUpdatesByMemberReactions`. |
| `videos` | Request captured, parser pending | Needs response fixture before enabling. |
| media categories | Experimental local filter only | Not Web UI-equivalent. |

Use `--experimental-local-filter` only when you explicitly want the legacy local
heuristic filtering for debugging. Local filters classify the generic Voyager
activity response and are not equivalent to LinkedIn Web UI category tabs.

Use `--debug-shape --json` with `profile activity` to inspect safe structural
response metadata for capture/debug work. For `--category posts`, `comments`,
and `reactions`, debug-shape targets the GraphQL category endpoint. Debug-shape
output includes endpoint path/query, status, top-level keys, data and included
counts, example `$type` values, paging keys, and next-link presence. It does not
include cookies, CSRF tokens, authorization headers, full raw responses, names,
messages, or text.

Example unsupported JSON error for categories without verified Web UI response shapes:

```json
{
  "success": false,
  "error": {
    "code": "UNSUPPORTED",
    "message": "LinkedIn Web UI matching for category \"videos\" is not currently implemented. The previous implementation used local heuristics and may return incorrect results. Capture the Web UI request shape or retry with --experimental-local-filter if you explicitly want the legacy heuristic behavior."
  }
}
```

Captured videos request shape is documented for future parser work:

```text
includeWebMetadata=true&variables=(contentType:VIDEOS,profileUrn:urn%3Ali%3Afsd_profile%3AREDACTED,start:0,count:20,isLookBackWindowEnabled:false,moduleKey:creator_profile_videos_content_view%3Adesktop)&queryId=voyagerFeedDashProfileContentViewModels.7719f1c8daecffaa8a087a40a775e11c
```

#### Safe DevTools capture instructions

To help implement real UI-equivalent categories later, capture request shapes for
the Web UI tabs without sharing secrets:

1. Open LinkedIn in a browser and sign in.
2. Open DevTools > Network, enable Preserve log, and filter for Fetch/XHR.
3. Visit these pages for the target profile:
   - `https://www.linkedin.com/in/USERNAME/recent-activity/all/`
   - `https://www.linkedin.com/in/USERNAME/recent-activity/posts/`
   - `https://www.linkedin.com/in/USERNAME/recent-activity/comments/`
4. For each tab, capture only the request method, path, query parameters,
   non-secret header names, response status, top-level response keys, paging
   keys, data/included counts, and example `$type` values.
5. Do not share `Cookie`, `li_at`, `JSESSIONID`, `csrf-token`, authorization
   headers, full raw responses, names, messages, post text, comments, or other
   private content.

Category UI URL mapping:

| Category | UI URL |
|----------|--------|
| `all` | `https://www.linkedin.com/in/USERNAME/recent-activity/all/` |
| `posts` | `https://www.linkedin.com/in/USERNAME/recent-activity/posts/` |
| `images` | `https://www.linkedin.com/in/USERNAME/recent-activity/images/` |
| `videos` | `https://www.linkedin.com/in/USERNAME/recent-activity/videos/` |
| `documents` | `https://www.linkedin.com/in/USERNAME/recent-activity/documents/` |
| `events` | `https://www.linkedin.com/in/USERNAME/recent-activity/events/` |
| `reactions` | `https://www.linkedin.com/in/USERNAME/recent-activity/reactions/` |
| `comments` | `https://www.linkedin.com/in/USERNAME/recent-activity/comments/` |

## Agent Integration

All commands support `--json` flag for structured output, making it easy to integrate with AI agents like Claude Code.

### JSON Output Examples

```bash
# Get profile as JSON
lnk profile me --json
```

```json
{
  "success": true,
  "data": {
    "urn": "urn:li:fsd_profile:ACoAAAA",
    "firstName": "John",
    "lastName": "Doe",
    "headline": "Software Engineer",
    "profileUrl": "https://www.linkedin.com/in/johndoe"
  }
}
```

```bash
# Search for people
lnk search people "iOS developer" --json
```

```json
{
  "success": true,
  "data": [
    {
      "urn": "urn:li:member:123456",
      "firstName": "Jane",
      "lastName": "Smith",
      "headline": "Senior iOS Developer",
      "location": "San Francisco",
      "profileUrl": "https://www.linkedin.com/in/janesmith"
    }
  ]
}
```

### Error Format

```json
{
  "success": false,
  "error": {
    "code": "AUTH_EXPIRED",
    "message": "Session cookie expired. Run: lnk auth login"
  }
}
```

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Authentication failure |

## Known Limitations

LinkedIn frequently changes their internal APIs. Some features may not work reliably:

| Feature | Status | Notes |
|---------|--------|-------|
| Profile viewing | ✅ Working | |
| Post create/delete | ✅ Working | |
| Search people | ✅ Working | |
| Search companies | ✅ Working | |
| Feed | ⚠️ Limited | LinkedIn has restricted feed API access |
| Messaging | ⚠️ Limited | LinkedIn has restricted messaging API access |

## Configuration

Credentials are stored in:
- **macOS/Linux**: `~/.config/lnk/credentials.json`

You can customize the location using the `XDG_CONFIG_HOME` environment variable.

## Supported Platforms

| Platform | Safari | Chrome | Firefox | Brave | Arc |
|----------|--------|--------|---------|-------|-----|
| macOS | Yes | Yes | Yes | Yes | Yes |
| Linux | No | Yes | Yes | Yes | No |

## Troubleshooting

### "Permission denied reading Safari cookies"

Grant Full Disk Access to your terminal:
1. Open System Preferences > Privacy & Security > Full Disk Access
2. Add your terminal application (Terminal, iTerm2, etc.)

### "LinkedIn requires verification"

LinkedIn may require captcha or 2FA verification after multiple login attempts. Solutions:
1. Wait a few minutes and try again
2. Use browser cookie authentication instead
3. Log in via browser first, then extract cookies

### "No LinkedIn cookies found"

Make sure you:
1. Are logged into LinkedIn in the specified browser
2. The browser is closed (or database is not locked)
3. Have the correct permissions to read browser data

## Development

### Building

```bash
go build -o lnk ./cmd/lnk
```

Cross-platform binary targets:

```bash
GOOS=linux GOARCH=amd64 go build -o dist/lnk_linux_amd64 ./cmd/lnk
GOOS=linux GOARCH=arm64 go build -o dist/lnk_linux_arm64 ./cmd/lnk
GOOS=darwin GOARCH=amd64 go build -o dist/lnk_darwin_amd64 ./cmd/lnk
GOOS=darwin GOARCH=arm64 go build -o dist/lnk_darwin_arm64 ./cmd/lnk
```

### Testing

```bash
go test ./...
```

## Disclaimer

This tool uses LinkedIn's unofficial Voyager API. It is:

- **Not affiliated with** LinkedIn or Microsoft
- **Not authorized, maintained, sponsored, or endorsed** by LinkedIn
- **Use at your own risk** - may violate LinkedIn's Terms of Service

LinkedIn may temporarily or permanently ban accounts that use unofficial APIs. Use responsibly.

## License

MIT License - see [LICENSE](LICENSE) for details.

## Credits

- Inspired by [steipete/bird](https://github.com/steipete/bird)
- Uses LinkedIn's internal Voyager API patterns from [linkedin-api](https://github.com/tomquirk/linkedin-api)
