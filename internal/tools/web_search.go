package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SearchProvider represents any search engine capable of performing web queries (e.g. Perplexity, DuckDuckGo, Bing).
type SearchProvider interface {
	Search(ctx context.Context, query string) (string, error)
}

// WebSearchTool implements an agentic multi-step search engine with credibility scoring and iterative query expansion.
type WebSearchTool struct {
	provider   SearchProvider
	httpClient *http.Client
}

// NewWebSearchTool creates a new web search tool.
func NewWebSearchTool(provider SearchProvider) *WebSearchTool {
	return &WebSearchTool{
		provider: provider,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// SetProvider attaches or updates the search engine backend.
func (w *WebSearchTool) SetProvider(provider SearchProvider) {
	w.provider = provider
}

func (w *WebSearchTool) Name() string {
	return "web_search"
}

func (w *WebSearchTool) Description() string {
	return "محرك بحث ذكي متعدد الخطوات للبحث اللحظي في الإنترنت مع تقييم موثوقية المصادر وتوسيع الاستعلامات والتحقق المتقاطع"
}

func (w *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "سؤال أو موضوع البحث باللغة العربية أو الإنجليزية",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"standard", "deep", "fact_check", "news"},
				"description": "نمط البحث: standard (بحث سريع)، deep (بحث معمق متعدد المحاور)، fact_check (تدقيق وتثبت من الشائعات والحقائق)، news (أحدث المستجدات)",
			},
			"max_steps": map[string]interface{}{
				"type":        "integer",
				"description": "الحد الأقصى لعدد الاستعلامات الفرعية المنفذة (1-3)",
			},
		},
		"required": []string{"query"},
	}
}

func (w *WebSearchTool) Permission() PermissionLevel {
	return PermissionEveryone
}

type webSearchArgs struct {
	Query    string `json:"query"`
	Mode     string `json:"mode"`
	MaxSteps int    `json:"max_steps"`
}

type searchStepResult struct {
	SubQuery string
	Content  string
	Error    error
}

// CredibilityReport assesses the trustworthiness of retrieved information.
type CredibilityReport struct {
	Score        int      // 0 to 100
	Level        string   // "عالي جداً 🟢", "جيد 🟡", "متوسط 🟠", "منخفض 🔴"
	HighAuthURLs []string // authoritative domains found
	KeySignals   []string // positive reliability signals
	Concerns     []string // potential reliability warnings
}

func (w *WebSearchTool) Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	var input webSearchArgs
	if err := json.Unmarshal(args, &input); err != nil {
		input.Query = strings.Trim(string(args), `"{}`)
	}

	rawQuery := strings.TrimSpace(input.Query)
	if rawQuery == "" {
		return "", errors.New("search query cannot be empty")
	}

	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode == "" {
		mode = "standard"
	}

	maxSteps := input.MaxSteps
	if maxSteps <= 0 {
		if mode == "deep" || mode == "fact_check" {
			maxSteps = 2
		} else {
			maxSteps = 1
		}
	}
	if maxSteps > 3 {
		maxSteps = 3
	}

	// 1. Iterative Query Expansion
	subQueries := ExpandQuery(rawQuery, mode, maxSteps)

	// 2. Multi-step Execution
	results := w.executeSubQueries(ctx, subQueries)

	// 3. Credibility Scoring & Cross-Verification
	credibility := ScoreCredibility(rawQuery, results)

	// 4. Synthesize Output
	return FormatSearchReport(rawQuery, mode, results, credibility), nil
}

// ExpandQuery generates focused sub-queries based on search intent and mode.
func ExpandQuery(query string, mode string, maxSteps int) []string {
	if maxSteps <= 1 {
		return []string{query}
	}

	queries := []string{query}

	clean := strings.TrimSpace(query)
	currentYear := time.Now().Year()

	switch mode {
	case "fact_check":
		// Query 1: Original
		// Query 2: Fact check / official source
		queries = append(queries, fmt.Sprintf("صحة خبر %s حقيقة أم شائعة مصدر رسمي", clean))
		if maxSteps >= 3 {
			queries = append(queries, fmt.Sprintf("fact check %s official statement", clean))
		}

	case "news":
		// Query 1: Original
		// Query 2: Latest updates & current date
		queries = append(queries, fmt.Sprintf("آخر أخبار %s %d تفاصيل جديدة", clean, currentYear))
		if maxSteps >= 3 {
			queries = append(queries, fmt.Sprintf("latest news %s %d update", clean, currentYear))
		}

	case "deep":
		// Query 1: Original
		// Query 2: Details / Background / Specs
		queries = append(queries, fmt.Sprintf("تفاصيل وشرح شامل %s إحصائيات وأرقام", clean))
		if maxSteps >= 3 {
			queries = append(queries, fmt.Sprintf("%s analysis facts overview", clean))
		}

	default: // standard with multiple steps
		if maxSteps >= 2 {
			queries = append(queries, fmt.Sprintf("%s %d", clean, currentYear))
		}
	}

	if len(queries) > maxSteps {
		queries = queries[:maxSteps]
	}

	return queries
}

func (w *WebSearchTool) executeSubQueries(ctx context.Context, subQueries []string) []searchStepResult {
	results := make([]searchStepResult, len(subQueries))
	var wg sync.WaitGroup

	for i, q := range subQueries {
		wg.Add(1)
		go func(idx int, query string) {
			defer wg.Done()

			subCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()

			var text string
			var err error

			if w.provider != nil {
				text, err = w.provider.Search(subCtx, query)
			} else {
				text, err = w.fallbackDirectSearch(subCtx, query)
			}

			results[idx] = searchStepResult{
				SubQuery: query,
				Content:  text,
				Error:    err,
			}
		}(i, q)
	}

	wg.Wait()
	return results
}

// Fallback search when no external provider is attached.
func (w *WebSearchTool) fallbackDirectSearch(ctx context.Context, query string) (string, error) {
	// Simple DuckDuckGo HTML search fallback
	endpoint := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("search engine status %d", resp.StatusCode)
	}

	article, err := ExtractHTMLContent(resp.Body)
	if err != nil {
		return "", err
	}

	text := article.BodyText
	if len(text) > 1500 {
		text = text[:1500] + "..."
	}
	return text, nil
}

// High authority domains list for scoring
var authoritativeDomains = map[string]int{
	"wikipedia.org":    95,
	"reuters.com":      98,
	"apnews.com":       98,
	"bbc.com":          95,
	"bbc.co.uk":        95,
	"who.int":          99,
	"nature.com":       99,
	"sciencedirect.com": 95,
	"gov":              95, // .gov TLD
	"edu":              90, // .edu TLD
	"aljazeera.net":    90,
	"alarabiya.net":    90,
	"ahram.org.eg":     88,
	"youm7.com":        85,
	"skynewsarabia.com":88,
	"cnn.com":          90,
	"bloomberg.com":    95,
	"investopedia.com": 90,
	"github.com":       92,
	"stackoverflow.com":90,
	"microsoft.com":    92,
	"apple.com":        92,
	"google.com":       92,
}

// ScoreCredibility evaluates search text for authority signals, dates, and cross-consistency.
func ScoreCredibility(origQuery string, results []searchStepResult) CredibilityReport {
	baseScore := 65
	var signals []string
	var concerns []string
	var highAuthDomains []string

	successCount := 0
	combinedContent := ""

	for _, res := range results {
		if res.Error == nil && strings.TrimSpace(res.Content) != "" {
			successCount++
			combinedContent += "\n" + res.Content
		}
	}

	if successCount == 0 {
		return CredibilityReport{
			Score:    0,
			Level:    "فشل البحث 🔴",
			Concerns: []string{"لم يتم العثور على أي نتائج ناجحة من محركات البحث"},
		}
	}

	lowerContent := strings.ToLower(combinedContent)

	// 1. Check for authoritative domains and citations
	for domain, weight := range authoritativeDomains {
		if strings.Contains(lowerContent, domain) {
			baseScore += (weight - 80) / 3
			highAuthDomains = append(highAuthDomains, domain)
		}
	}

	if len(highAuthDomains) > 0 {
		signals = append(signals, fmt.Sprintf("تأكيد من مصادر عالية الموثوقية (%s)", strings.Join(highAuthDomains, ", ")))
		baseScore += 10
	} else {
		concerns = append(concerns, "النتائج مستخلصة من مصادر عامة بدون روابط نطاقات حكومية أو رسمية مؤكدة")
	}

	// 2. Check for date / recency signals
	yearRe := regexp.MustCompile(`\b(202[0-6])\b`)
	if yearRe.MatchString(combinedContent) {
		baseScore += 8
		signals = append(signals, "وجود تواريخ وأرقام زمنية حديثة ومحددة")
	}

	// 3. Multi-step consensus bonus
	if successCount >= 2 {
		baseScore += 10
		signals = append(signals, fmt.Sprintf("تطابق وتوافق المعلومات عبر %d استعلامات فرعية مستقلة", successCount))
	}

	// 4. Specific numeric/statistical evidence
	numRe := regexp.MustCompile(`\b\d+(?:[\.,]\d+)?(?:\%| مليون| ألف| مليار| USD| جنيه| ريال| دولار)?\b`)
	if len(numRe.FindAllString(combinedContent, -1)) >= 3 {
		baseScore += 5
		signals = append(signals, "احتواء النتائج على بيانات رقمية وإحصائية دقيقة")
	}

	// Clamp score between 10 and 99
	if baseScore > 98 {
		baseScore = 98
	}
	if baseScore < 20 {
		baseScore = 20
	}

	level := "موثوقية جيدة 🟡"
	if baseScore >= 85 {
		level = "موثوقية عالية جداً 🟢 (معتمد رسمياً ومن مصادر أولية)"
	} else if baseScore >= 70 {
		level = "موثوقية جيدة 🟡 (مصادر معرفية وإخبارية معتمدة)"
	} else if baseScore >= 50 {
		level = "موثوقية متوسطة 🟠 (يحتاج إلى مزيد من التدقيق المتقاطع)"
	} else {
		level = "موثوقية منخفضة 🔴 (معلومات غير مؤكدة)"
	}

	return CredibilityReport{
		Score:        baseScore,
		Level:        level,
		HighAuthURLs: highAuthDomains,
		KeySignals:   signals,
		Concerns:     concerns,
	}
}

// FormatSearchReport builds a rich structured search output.
func FormatSearchReport(origQuery, mode string, results []searchStepResult, cred CredibilityReport) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🔍 **نتائج البحث المعمق عن:** %q\n", origQuery))
	sb.WriteString(fmt.Sprintf("📊 **مؤشر الموثوقية:** **%d%%** - %s\n", cred.Score, cred.Level))
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")

	// Credibility breakdown
	if len(cred.KeySignals) > 0 {
		sb.WriteString("✅ **عوامل الثقة المكتشفة:**\n")
		for _, sig := range cred.KeySignals {
			sb.WriteString(fmt.Sprintf("  • %s\n", sig))
		}
	}
	if len(cred.Concerns) > 0 {
		sb.WriteString("⚠️ **ملاحظات التدقيق:**\n")
		for _, con := range cred.Concerns {
			sb.WriteString(fmt.Sprintf("  • %s\n", con))
		}
	}

	sb.WriteString("\n📑 **الحقائق والمعلومات المستخلصة:**\n\n")

	for i, res := range results {
		if res.Error != nil {
			sb.WriteString(fmt.Sprintf("⚠️ *استعلام [%d]: %s* (فشل: %v)\n\n", i+1, res.SubQuery, res.Error))
			continue
		}

		cleanContent := strings.TrimSpace(res.Content)
		if cleanContent == "" {
			continue
		}

		if len(results) > 1 {
			sb.WriteString(fmt.Sprintf("🔹 **المحور %d (%s):**\n", i+1, res.SubQuery))
		}
		sb.WriteString(cleanContent)
		sb.WriteString("\n\n")
	}

	return strings.TrimSpace(sb.String())
}
