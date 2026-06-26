package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Post represents the data structure for a blog post, regardless of
// whether it was retrieved via the Blogger v3 API or HTML crawling.
// Images/Content are here so future embedded-script widgets (beyond the
// playlist) have somewhere to pull from without another schema change.
type Post struct {
	Title    string   `json:"title"`
	URL      string   `json:"url"`
	VideoURL string   `json:"video_url,omitempty"`
	Images   []string `json:"images,omitempty"`
	Content  string   `json:"content,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// Constants
const (
	userAgent         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	maxRetries        = 3
	crawlWorkers      = 5 // Concurrent workers for HTML crawl mode only
	requestTimeout    = 10 * time.Second
	defaultMaxResults = 500 // Blogger API page size cap
	bloggerAPIBase    = "https://www.googleapis.com/blogger/v3"
)

var httpClient = &http.Client{Timeout: requestTimeout}

// ---------------------------------------------------------------------------
// Blogger v3 API types
// ---------------------------------------------------------------------------

type apiBlog struct {
	ID string `json:"id"`
}

type apiPost struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Labels  []string `json:"labels"`
	URL     string   `json:"url"`
}

type apiPostList struct {
	Items         []apiPost `json:"items"`
	NextPageToken string    `json:"nextPageToken"`
}

// ---------------------------------------------------------------------------
// Shared HTTP helpers
// ---------------------------------------------------------------------------

// doWithRetry performs a GET request with retry + exponential backoff,
// returning the response only on an actual 200 OK. Unlike the old
// implementation, a non-200 status is treated as a failure and retried
// instead of being silently parsed as if it succeeded.
func doWithRetry(targetURL string) (*http.Response, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)

	var resp *http.Response
	var lastErr error
	for retry := 0; retry < maxRetries; retry++ {
		resp, lastErr = httpClient.Do(req)
		if lastErr == nil && resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		if lastErr == nil {
			// Got a response, just not a 200 — drain/close before retrying.
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		time.Sleep(time.Second * time.Duration(retry+1)) // Exponential backoff
	}
	return nil, fmt.Errorf("error fetching %s after %d attempts: %v", targetURL, maxRetries, lastErr)
}

// fetchHTML fetches and parses a page as an HTML document (used by crawl mode).
func fetchHTML(targetURL string) (*goquery.Document, error) {
	resp, err := doWithRetry(targetURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing HTML: %v", err)
	}
	return doc, nil
}

// fetchJSON fetches a URL and decodes the JSON body into out (used by API mode).
func fetchJSON(targetURL string, out interface{}) error {
	resp, err := doWithRetry(targetURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// extractVideoFromHTML pulls the first <iframe src> out of a post's HTML
// body. This replaces the old "fetch the post page and find an iframe"
// step — the API already gives us the body, so this is just local parsing.
func extractVideoFromHTML(htmlContent string) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}
	src, _ := doc.Find("iframe").First().Attr("src")
	return strings.TrimSpace(src)
}

// extractImagesFromHTML pulls every <img src> out of a post's HTML body,
// for widgets that want post imagery (carousels, thumbnails, etc.).
func extractImagesFromHTML(htmlContent string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}
	var images []string
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists {
			src = strings.TrimSpace(src)
			if src != "" {
				images = append(images, src)
			}
		}
	})
	return images
}

// ---------------------------------------------------------------------------
// Primary path: Blogger v3 API
// ---------------------------------------------------------------------------

// resolveBlogID looks up a blog's numeric ID from its public URL.
func resolveBlogID(baseURL, apiKey string) (string, error) {
	lookupURL := fmt.Sprintf("%s/blogs/byurl?url=%s&key=%s",
		bloggerAPIBase, url.QueryEscape(baseURL), url.QueryEscape(apiKey))

	var blog apiBlog
	if err := fetchJSON(lookupURL, &blog); err != nil {
		return "", fmt.Errorf("resolving blog ID for %s: %v", baseURL, err)
	}
	if blog.ID == "" {
		return "", fmt.Errorf("no blog found for URL %s (check the URL and API key)", baseURL)
	}
	return blog.ID, nil
}

// crawlViaAPI is the new primary path: paginate Blogger v3's posts.list
// endpoint and build Post records directly from the structured response.
func crawlViaAPI(baseURL, apiKey, outputFile string, maxResults int) error {
	if apiKey == "" {
		return fmt.Errorf("API mode requires an API key (-apikey flag or BLOGGER_API_KEY env var)")
	}

	blogID, err := resolveBlogID(baseURL, apiKey)
	if err != nil {
		return err
	}
	log.Printf("Resolved blog ID %s for %s", blogID, baseURL)

	var posts []Post
	pageToken := ""

	for {
		listURL := fmt.Sprintf("%s/blogs/%s/posts?key=%s&maxResults=%d&fetchBodies=true",
			bloggerAPIBase, blogID, url.QueryEscape(apiKey), maxResults)
		if pageToken != "" {
			listURL += "&pageToken=" + url.QueryEscape(pageToken)
		}

		var page apiPostList
		if err := fetchJSON(listURL, &page); err != nil {
			return fmt.Errorf("error fetching posts page: %v", err)
		}

		for _, item := range page.Items {
			posts = append(posts, Post{
				Title:    strings.TrimSpace(item.Title),
				URL:      item.URL,
				VideoURL: extractVideoFromHTML(item.Content),
				Images:   extractImagesFromHTML(item.Content),
				Content:  item.Content,
				Tags:     item.Labels,
			})
		}
		log.Printf("Fetched %d posts (running total: %d)", len(page.Items), len(posts))

		if page.NextPageToken == "" {
			break
		}
		pageToken = page.NextPageToken
	}

	return writeToJSON(posts, outputFile)
}

// ---------------------------------------------------------------------------
// Fallback path: HTML crawling (formerly the only behavior)
// ---------------------------------------------------------------------------

// crawlPage crawls a single listing page and extracts post URLs, then
// recurses into the "Older Posts" link if one exists.
func crawlPage(pageURL string, postChan chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	doc, err := fetchHTML(pageURL)
	if err != nil {
		log.Printf("error crawling page %s: %v", pageURL, err)
		return
	}

	doc.Find("h3.post-title a").Each(func(i int, s *goquery.Selection) {
		postURL, exists := s.Attr("href")
		if exists {
			if !strings.HasPrefix(postURL, "http") {
				postURL = pageURL + postURL
			}
			log.Printf("Found post: %s", postURL)
			postChan <- postURL
		}
	})

	nextPageLink := doc.Find("a.blog-pager-older-link")
	if nextPageLink.Length() > 0 {
		nextPageURL, exists := nextPageLink.Attr("href")
		if exists {
			if !strings.HasPrefix(nextPageURL, "http") {
				nextPageURL = pageURL + nextPageURL
			}
			log.Printf("Found next page: %s", nextPageURL)
			wg.Add(1)
			go crawlPage(nextPageURL, postChan, wg)
		}
	} else {
		log.Println("No more posts found. Exiting")
	}
}

// extractPostData scrapes title/video/images/content/tags from a single
// post page's HTML.
func extractPostData(postURL string) (Post, error) {
	doc, err := fetchHTML(postURL)
	if err != nil {
		return Post{}, fmt.Errorf("error fetching post page: %v", err)
	}

	title := doc.Find("h3.post-title").First().Text()
	videoURL, _ := doc.Find("iframe").First().Attr("src")

	body := doc.Find("div.post-body").First()
	content, _ := body.Html() // best-effort; empty if theme doesn't use this class

	var images []string
	body.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists {
			src = strings.TrimSpace(src)
			if src != "" {
				images = append(images, src)
			}
		}
	})

	var tags []string
	doc.Find("span.post-labels a").Each(func(i int, s *goquery.Selection) {
		tags = append(tags, strings.TrimSpace(s.Text()))
	})

	return Post{
		Title:    strings.TrimSpace(title),
		URL:      postURL,
		VideoURL: strings.TrimSpace(videoURL),
		Images:   images,
		Content:  content,
		Tags:     tags,
	}, nil
}

// worker processes post URLs and extracts data (HTML crawl mode only).
func worker(postChan <-chan string, resultsChan chan<- Post, wg *sync.WaitGroup) {
	defer wg.Done()
	for postURL := range postChan {
		post, err := extractPostData(postURL)
		if err != nil {
			log.Printf("error extracting data from %s: %v", postURL, err)
			continue
		}
		resultsChan <- post
	}
}

// crawlViaHTML is the original behavior, now opt-in via -mode=crawl.
func crawlViaHTML(baseURL, outputFile string) error {
	postChan := make(chan string, 100)
	resultsChan := make(chan Post, 100)

	var crawlerWg sync.WaitGroup
	var workerWg sync.WaitGroup

	crawlerWg.Add(1)
	go crawlPage(baseURL, postChan, &crawlerWg)

	for i := 0; i < crawlWorkers; i++ {
		workerWg.Add(1)
		go worker(postChan, resultsChan, &workerWg)
	}

	var posts []Post
	var resultsWg sync.WaitGroup
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for post := range resultsChan {
			posts = append(posts, post)
		}
	}()

	crawlerWg.Wait()
	close(postChan)
	workerWg.Wait()
	close(resultsChan)
	resultsWg.Wait()

	return writeToJSON(posts, outputFile)
}

// ---------------------------------------------------------------------------
// JSON output (shared by both modes)
// ---------------------------------------------------------------------------

func writeToJSON(posts []Post, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("error creating JSON file: %v", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(posts); err != nil {
		return fmt.Errorf("error writing JSON: %v", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	mode := flag.String("mode", "api", `Retrieval mode: "api" (Blogger v3 API, default) or "crawl" (legacy HTML scraping fallback)`)
	apiKey := flag.String("apikey", os.Getenv("BLOGGER_API_KEY"), "Blogger v3 API key (required for -mode=api; defaults to $BLOGGER_API_KEY)")
	baseURLFlag := flag.String("baseurl", "", "Base URL of the Blogger site (e.g. https://yoursitename.blogspot.com)")
	outputFlag := flag.String("output", "", "JSON output filename")
	maxResults := flag.Int("maxresults", defaultMaxResults, "Posts per page when using -mode=api (max 500)")
	flag.Parse()

	// Backward compatibility: allow positional args <baseURL> <outputFile>
	// just like the original tool, in case anything still calls it that way.
	args := flag.Args()
	baseURL := *baseURLFlag
	outputFile := *outputFlag
	if baseURL == "" && len(args) > 0 {
		baseURL = args[0]
	}
	if outputFile == "" && len(args) > 1 {
		outputFile = args[1]
	}

	if baseURL == "" || outputFile == "" {
		log.Fatalf(`Usage: %s -baseurl <url> -output <file.json> [-mode api|crawl] [-apikey KEY]
Example (API mode, default):
  %s -baseurl https://iandiwatching.blogspot.com -output posts.json -apikey YOUR_KEY
Example (legacy crawl mode):
  %s -mode crawl -baseurl https://iandiwatching.blogspot.com -output posts.json`,
			os.Args[0], os.Args[0], os.Args[0])
	}

	startTime := time.Now()
	var err error

	switch *mode {
	case "api":
		err = crawlViaAPI(baseURL, *apiKey, outputFile, *maxResults)
	case "crawl":
		err = crawlViaHTML(baseURL, outputFile)
	default:
		log.Fatalf(`unknown -mode %q, expected "api" or "crawl"`, *mode)
	}

	if err != nil {
		log.Fatalf("error: %v", err)
	}

	log.Printf("Done in %v.", time.Since(startTime))
}
