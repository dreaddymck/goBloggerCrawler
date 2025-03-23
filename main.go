package main

import (
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Post represents the data structure for a blog post
type Post struct {
	Title    string
	VideoURL string
	Tags     []string
}

// Constants
const (
	userAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	maxRetries = 3
	workers    = 5 // Number of concurrent workers for crawling
	timeout    = 10 * time.Second
)

var (
	httpClient = &http.Client{
		Timeout: timeout,
	}
)

// fetchURL fetches the HTML content of a given URL
func fetchURL(url string) (*goquery.Document, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating request: %v", err)
	}
	req.Header.Set("User-Agent", userAgent)

	var resp *http.Response
	for retry := 0; retry < maxRetries; retry++ {
		resp, err = httpClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
		time.Sleep(time.Second * time.Duration(retry+1)) // Exponential backoff
	}
	if err != nil {
		return nil, fmt.Errorf("error fetching URL: %v", err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error parsing HTML: %v", err)
	}
	return doc, nil
}

// extractPostData extracts the title, video URL, and tags from a post page
func extractPostData(url string) (Post, error) {
	doc, err := fetchURL(url)
	if err != nil {
		return Post{}, fmt.Errorf("error fetching post page: %v", err)
	}

	// Debug: Print the HTML of the post page
	// html, _ := doc.Html()
	// log.Printf("Post page HTML: %s\n", html)

	// Extract title
	title := doc.Find("h3.post-title").First().Text() // Updated selector

	// Extract video URL (assuming it's in an iframe)
	videoURL, _ := doc.Find("iframe").First().Attr("src")

	// Extract tags (labels)
	var tags []string
	doc.Find("span.post-labels a").Each(func(i int, s *goquery.Selection) { // Updated selector
		tags = append(tags, strings.TrimSpace(s.Text()))
	})

	return Post{
		Title:    strings.TrimSpace(title),
		VideoURL: strings.TrimSpace(videoURL),
		Tags:     tags,
	}, nil
}

// crawlPage crawls a single page and extracts post URLs
func crawlPage(url string, postChan chan<- string, wg *sync.WaitGroup) {
	defer wg.Done()

	doc, err := fetchURL(url)
	if err != nil {
		log.Printf("error crawling page %s: %v", url, err)
		return
	}

	// Debug: Print the HTML of the page
	// html, _ := doc.Html()
	// log.Printf("Page HTML: %s\n", html)

	// Extract post URLs
	doc.Find("h3.post-title a").Each(func(i int, s *goquery.Selection) { // Updated selector
		postURL, exists := s.Attr("href")
		if exists {
			// Ensure the post URL is absolute
			if !strings.HasPrefix(postURL, "http") {
				postURL = url + postURL
			}
			log.Printf("Found post: %s", postURL)
			postChan <- postURL
		}
	})

	// Find the "More Posts" link and crawl the next page
	nextPageLink := doc.Find("a.blog-pager-older-link")
	if nextPageLink.Length() > 0 {
		nextPageURL, exists := nextPageLink.Attr("href")
		if exists {
			// Ensure the next page URL is absolute
			if !strings.HasPrefix(nextPageURL, "http") {
				nextPageURL = url + nextPageURL
			}
			log.Printf("Found next page: %s", nextPageURL)
			wg.Add(1)
			go crawlPage(nextPageURL, postChan, wg)
		}
	} else {
		log.Println("No more posts found. Exiting")
	}
}

// worker processes post URLs and extracts data
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

// writeToCSV writes the collected posts to a CSV file
func writeToCSV(posts []Post, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("error creating CSV file: %v", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	header := []string{"Title", "Video URL", "Tags"}
	if err := writer.Write(header); err != nil {
		return fmt.Errorf("error writing CSV header: %v", err)
	}

	// Write rows
	for _, post := range posts {
		row := []string{post.Title, post.VideoURL, strings.Join(post.Tags, ", ")}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("error writing CSV row: %v", err)
		}
	}

	return nil
}

func main() {
	// Check for required command-line arguments
	if len(os.Args) < 3 {
		log.Fatalf("Usage: %s <baseURL> <outputFile>\nExample: %s https://iandiwatching.blogspot.com posts.csv", os.Args[0], os.Args[0])
	}

	baseURL := os.Args[1]
	outputFile := os.Args[2]

	startTime := time.Now()

	// Channels for communication
	postChan := make(chan string, 100)  // Buffered channel for post URLs
	resultsChan := make(chan Post, 100) // Buffered channel for post data

	// WaitGroups for synchronization
	var crawlerWg sync.WaitGroup
	var workerWg sync.WaitGroup

	// Start crawling the initial page
	crawlerWg.Add(1)
	go crawlPage(baseURL, postChan, &crawlerWg)

	// Start worker goroutines
	for i := 0; i < workers; i++ {
		workerWg.Add(1)
		go worker(postChan, resultsChan, &workerWg)
	}

	// Collect results in a separate goroutine
	var posts []Post
	var resultsWg sync.WaitGroup
	resultsWg.Add(1)
	go func() {
		defer resultsWg.Done()
		for post := range resultsChan {
			posts = append(posts, post)
		}
	}()

	// Wait for all crawlers to finish
	crawlerWg.Wait()
	close(postChan) // Close postChan to signal workers to exit

	// Wait for all workers to finish
	workerWg.Wait()
	close(resultsChan) // Close resultsChan to signal results collector to exit

	// Wait for results collector to finish
	resultsWg.Wait()

	// Write results to CSV
	if err := writeToCSV(posts, outputFile); err != nil {
		log.Fatalf("error writing to CSV: %v", err)
	}

	log.Printf("Crawling completed in %v. Total posts: %d", time.Since(startTime), len(posts))
}
