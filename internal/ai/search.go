package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SearchEngine handles live web searches using DuckDuckGo with optional Perplexity fallback.
type SearchEngine struct {
	apiKey      string
	searchModel string
	httpClient  *http.Client
}

// NewSearchEngine creates a new search engine instance.
func NewSearchEngine(apiKey string, searchModel string) *SearchEngine {
	if searchModel == "" {
		searchModel = "perplexity/sonar"
	}
	return &SearchEngine{
		apiKey:      apiKey,
		searchModel: searchModel,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// CleanDDGURL extracts the destination target URL from DuckDuckGo redirect parameters.
func CleanDDGURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if u, err := url.Parse(raw); err == nil {
		if uddg := u.Query().Get("uddg"); uddg != "" {
			return uddg
		}
	}
	return raw
}

// SearchDuckDuckGo queries the DuckDuckGo HTML endpoint and extracts top 3 to 5 snippets using goquery.
func SearchDuckDuckGo(ctx context.Context, query string) (string, error) {
	cleanQuery := strings.TrimSpace(query)
	if cleanQuery == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	searchURL := "https://html.duckduckgo.com/html/?q=" + url.QueryEscape(cleanQuery)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create duckduckgo request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "ar,en-US;q=0.9,en;q=0.8")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("duckduckgo search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("duckduckgo search returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to parse duckduckgo HTML: %w", err)
	}

	var results []string
	count := 0

	doc.Find(".result").Each(func(i int, s *goquery.Selection) {
		if count >= 5 {
			return
		}

		snippet := strings.TrimSpace(s.Find(".result__snippet").Text())
		if snippet == "" {
			return
		}

		title := strings.TrimSpace(s.Find(".result__title").Text())
		link, _ := s.Find(".result__url").Attr("href")
		if link == "" {
			link, _ = s.Find(".result__title a").Attr("href")
		}
		cleanLink := CleanDDGURL(link)

		count++
		resText := fmt.Sprintf("[%d] %s\nالمقتطف: %s", count, title, snippet)
		if cleanLink != "" {
			resText += fmt.Sprintf("\nالمصدر: %s", cleanLink)
		}
		results = append(results, resText)
	})

	if len(results) == 0 {
		return "لم يتم العثور على نتائج بحث كافية عبر DuckDuckGo.", nil
	}

	return strings.Join(results, "\n\n"), nil
}

type searchRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
}

// Search executes a real-time web query using DuckDuckGo first, falling back to Perplexity if configured.
func (s *SearchEngine) Search(ctx context.Context, query string) (string, error) {
	if s == nil {
		return SearchDuckDuckGo(ctx, query)
	}
	if strings.TrimSpace(query) == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	// 1. Primary: Free, real-time DuckDuckGo search
	ddgResult, ddgErr := SearchDuckDuckGo(ctx, query)
	if ddgErr == nil && ddgResult != "" && !strings.Contains(ddgResult, "لم يتم العثور") {
		return ddgResult, nil
	}

	// 2. Fallback: Perplexity on OpenRouter if apiKey is provided
	if s.apiKey != "" {
		reqBody := searchRequest{
			Model: s.searchModel,
			Messages: []openRouterMessage{
				{
					Role:    "system",
					Content: "You are a fast, factual real-time web search engine. Search the internet and provide a concise, factual, and accurate summary with relevant details, numbers, dates, and prices for the user's query.",
				},
				{
					Role:    "user",
					Content: query,
				},
			},
		}

		bodyBytes, err := json.Marshal(reqBody)
		if err == nil {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterAPIURL, bytes.NewReader(bodyBytes))
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+s.apiKey)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
				req.Header.Set("X-Title", "Nova WhatsApp Bot Search")

				resp, err := s.httpClient.Do(req)
				if err == nil {
					defer resp.Body.Close()
					respBytes, _ := io.ReadAll(resp.Body)
					if resp.StatusCode >= 200 && resp.StatusCode < 300 {
						var orResp openRouterResponse
						if err := json.Unmarshal(respBytes, &orResp); err == nil && len(orResp.Choices) > 0 {
							if str, ok := orResp.Choices[0].Message.Content.(string); ok && strings.TrimSpace(str) != "" {
								return strings.TrimSpace(str), nil
							}
						}
					}
				}
			}
		}
	}

	if ddgErr != nil {
		return "", ddgErr
	}
	return ddgResult, nil
}
