```markdown
# Blogger Web Crawler in Go

A concurrent web crawler that extracts post titles, embedded video URLs, and tags from Google Blogger sites, saving results to a CSV file.

## Features

- 🕷️ Recursively crawls blog pages following "More Posts" links
- 📊 Extracts post metadata (title, video URLs, tags)
- ⚡ Concurrent processing with worker pool pattern
- 📁 Outputs clean CSV data
- 🔧 Configurable via command-line arguments

## Requirements

- Go 1.16+
- Internet connection (to access target blog)

## Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/dreaddymck/goBloggerCrawler.git
   cd goBloggerCrawler
   ```

2. Install dependencies:
   ```bash
   go get github.com/PuerkitoBio/goquery
   ```

3. Build the executable:
   ```bash
   go build -o goBloggerCrawler
   ```

## Usage

```bash
./goBloggerCrawler <baseURL> <outputFile>
```

### Example
```bash
./goBloggerCrawler https://yoursitename.blogspot.com posts.csv
```

### Arguments
| Parameter    | Description                          | Required |
|--------------|--------------------------------------|----------|
| `baseURL`    | Starting URL of the Blogger site     | Yes      |
| `outputFile` | Filename for CSV output (e.g., `posts.csv`) | Yes      |

## Output Format
The CSV file will contain these columns:
1. Title
2. Video URL
3. Tags (comma-separated)

## Implementation Details

- **Concurrency Model**: Uses goroutines and channels for efficient crawling
- **Error Handling**: Retry mechanism for failed requests
- **Respectful Crawling**:
  - User-agent identification
  - Rate limiting with exponential backoff
- **Memory Efficient**: Streams results to CSV

## Customization

To modify what data is collected:
1. Edit the `Post` struct in `main.go`
2. Update the `extractPostData` function
3. Adjust the CSV writer in `writeToCSV`

## Limitations

- Doesn't execute JavaScript (won't work with JS-rendered content)
- Dependent on Blogger's HTML structure
- Rate limits not configurable (fixed at 3 retries)

## License

MIT License - See [LICENSE](https://mit-license.org/) for details.
