# Blogger Web Crawler in Go

A Go tool that retrieves post data — title, URL, embedded video, images, content, and tags — from a Google Blogger site and saves the results to JSON.

It has two retrieval modes:
- **`api`** (default) — uses the Blogger v3 API for structured, reliable data.
- **`crawl`** — the original concurrent HTML scraper, kept as a fallback for sites/situations where the API isn't an option.

## Features

- 🔌 **Blogger v3 API mode (default)** — paginates `posts.list` directly, no HTML scraping, no fragile selectors
- 🕷️ **HTML crawl mode (fallback)** — recursively follows "Older Posts" links with a concurrent worker pool, same as the original tool
- 📊 Extracts title, post URL, video URL, images, content, and tags
- 📁 Outputs structured JSON (`omitempty` keeps text-only posts lean)
- 🔧 Configurable via command-line flags

## Requirements

- Go 1.23+
- Internet connection (to access target blog / Blogger API)
- A Blogger v3 API key — only required for the default `api` mode (see below)

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/dreaddymck/goBloggerCrawler.git
   cd goBloggerCrawler
   ```

2. Build the executable:
   ```bash
   go build -o goBloggerCrawler
   ```
   Dependencies (`goquery`) are already declared in `go.mod`/`go.sum` — `go build` fetches them automatically.

## Getting a Blogger API key (for the default `api` mode)

1. Go to the [Google Cloud Console](https://console.cloud.google.com/).
2. Create a project (or use an existing one).
3. Enable the **Blogger API v3** under "APIs & Services" → "Library".
4. Go to "APIs & Services" → "Credentials" → "Create Credentials" → "API key".
5. (Recommended) Restrict the key to the Blogger API only.

Reading a public blog only needs the API key — no OAuth setup required.

Set it as an environment variable so it's not sitting in shell history or scripts:
```bash
export BLOGGER_API_KEY=your_key_here
```

## Usage

```bash
./goBloggerCrawler -baseurl <url> -output <file.json> [-mode api|crawl] [-apikey KEY]
```

### Examples

API mode (default):
```bash
./goBloggerCrawler -baseurl https://yoursitename.blogspot.com -output posts.json
```

Legacy HTML crawl mode:
```bash
./goBloggerCrawler -mode crawl -baseurl https://yoursitename.blogspot.com -output posts.json
```

Positional arguments still work as a shorthand for `-baseurl`/`-output`, mode defaults to `api`:
```bash
./goBloggerCrawler https://yoursitename.blogspot.com posts.json
```

### Flags
| Flag          | Description                                                              | Default               |
|---------------|---------------------------------------------------------------------------|------------------------|
| `-baseurl`    | Starting URL of the Blogger site                                         | *(required)*           |
| `-output`     | Filename for JSON output (e.g., `posts.json`)                            | *(required)*           |
| `-mode`       | `api` (Blogger v3 API) or `crawl` (legacy HTML scraping)                 | `api`                   |
| `-apikey`     | Blogger v3 API key (only used in `api` mode)                             | `$BLOGGER_API_KEY` env |
| `-maxresults` | Posts per page when paginating the API (max allowed by Blogger is 500)   | `500`                   |

## Output Format

Output is a JSON array of post objects:

```json
[
  {
    "title": "Track One",
    "url": "https://yoursitename.blogspot.com/2024/01/track-one.html",
    "video_url": "https://www.youtube.com/embed/abc12345678",
    "images": ["https://...jpg"],
    "content": "<div>...raw post HTML...</div>",
    "tags": ["reggae", "dub"]
  }
]
```

`video_url`, `images`, `content`, and `tags` are omitted entirely for a post if empty — text-only posts won't carry empty arrays/strings.

## Implementation Details

**API mode:**
- Resolves the blog's numeric ID via `blogs/byurl`, then paginates `blogs/{id}/posts` (`fetchBodies=true`) using `nextPageToken`.
- `title`, `url`, and `tags` come straight from the API response.
- `video_url` and `images` are extracted locally from the API's `content` HTML (first `<iframe src>`, all `<img src>`) — no extra network calls per post.

**Crawl mode:**
- Concurrency model: goroutines + channels, with a worker pool fanning out post-page fetches.
- Error handling: retries with exponential backoff; a non-200 response is treated as a failure and retried (not silently parsed as success).
- Respectful crawling: identifies itself with a User-Agent header.
- Selectors are tied to Blogger's default template (`h3.post-title`, `div.post-body`, `span.post-labels a`, `a.blog-pager-older-link`) — custom themes may need selector changes.

## Customization

To modify what data is collected:
1. Edit the `Post` struct in `main.go`.
2. For API mode: update `crawlViaAPI` / `extractVideoFromHTML` / `extractImagesFromHTML`.
3. For crawl mode: update `extractPostData`.
4. Adjust `writeToJSON` if you need a different output shape.

## Limitations

- Crawl mode doesn't execute JavaScript (won't work with JS-rendered content) and is dependent on Blogger's HTML structure.
- API mode requires a Google Cloud API key and is subject to Blogger API quotas.
- Retry count is fixed at 3 attempts (not currently configurable via flag).

## License

MIT License - See [LICENSE](https://mit-license.org/) for details.