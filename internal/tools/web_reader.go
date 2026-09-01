package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// WebReaderTool extracts readable article text and metadata from web URLs.
type WebReaderTool struct {
	httpClient      *http.Client
	allowPrivateIPs bool // For unit tests with httptest
	maxReadBytes    int64
}

// NewWebReaderTool creates a new web reader tool.
func NewWebReaderTool() *WebReaderTool {
	return &WebReaderTool{
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return nil
			},
		},
		allowPrivateIPs: false,
		maxReadBytes:    5 * 1024 * 1024, // 5MB max download
	}
}

// SetAllowPrivateIPs allows localhost/private IP testing in unit tests.
func (w *WebReaderTool) SetAllowPrivateIPs(allow bool) {
	w.allowPrivateIPs = allow
}

func (w *WebReaderTool) Name() string {
	return "web_reader"
}

func (w *WebReaderTool) Description() string {
	return "استخراج وقراءة نصوص المقالات والمحتوى من أي رابط صفحة ويب أو موقع بالكامل مع البيانات الوصفية"
}

func (w *WebReaderTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "رابط الصفحة أو المقال المطلوب قراءته (مثال: https://example.com/article)",
			},
			"max_length": map[string]interface{}{
				"type":        "integer",
				"description": "الحد الأقصى لعدد حروف النص المستخرج (الافتراضي: 4000 حرف)",
			},
		},
		"required": []string{"url"},
	}
}

func (w *WebReaderTool) Permission() PermissionLevel {
	return PermissionEveryone
}

type webReaderArgs struct {
	URL       string `json:"url"`
	MaxLength int    `json:"max_length"`
}

func (w *WebReaderTool) Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	var input webReaderArgs
	if err := json.Unmarshal(args, &input); err != nil {
		input.URL = strings.Trim(string(args), `"{}`)
	}

	targetURL := strings.TrimSpace(input.URL)
	if targetURL == "" {
		return "", errors.New("url cannot be empty")
	}

	maxLength := input.MaxLength
	if maxLength <= 0 || maxLength > 15000 {
		maxLength = 4000
	}

	return w.FetchAndExtract(ctx, targetURL, maxLength)
}

// IsPrivateIP checks if an IP address belongs to loopback, private RFC1918, or cloud metadata ranges.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	// Extra checks for IPv4 0.0.0.0/8, 169.254.0.0/16, 100.64.0.0/10, 198.18.0.0/15
	ipv4 := ip.To4()
	if ipv4 != nil {
		if ipv4[0] == 0 ||
			(ipv4[0] == 169 && ipv4[1] == 254) ||
			(ipv4[0] == 100 && (ipv4[1] >= 64 && ipv4[1] <= 127)) ||
			(ipv4[0] == 198 && (ipv4[1] == 18 || ipv4[1] == 19)) {
			return true
		}
	}
	return false
}

func (w *WebReaderTool) validateURL(rawURL string) (*url.URL, error) {
	if w == nil {
		return nil, errors.New("web reader tool is nil")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, errors.New("only http and https schemes are supported")
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return nil, errors.New("missing hostname in URL")
	}

	if !w.allowPrivateIPs {
		// If hostname is an IP literal
		if ip := net.ParseIP(hostname); ip != nil {
			if IsPrivateIP(ip) {
				return nil, fmt.Errorf("access to private/local network address %s is forbidden", ip.String())
			}
		} else {
			// Resolve IP to prevent SSRF
			ips, err := net.LookupIP(hostname)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve hostname %s: %w", hostname, err)
			}
			for _, ip := range ips {
				if IsPrivateIP(ip) {
					return nil, fmt.Errorf("access to private/local network address %s (%s) is forbidden", hostname, ip.String())
				}
			}
		}
	}

	return parsed, nil
}

// ExtractedArticle represents structured page content.
type ExtractedArticle struct {
	Title       string
	Description string
	Author      string
	BodyText    string
}

// FetchAndExtract retrieves and parses web page content.
func (w *WebReaderTool) FetchAndExtract(ctx context.Context, rawURL string, maxLength int) (string, error) {
	parsedURL, err := w.validateURL(rawURL)
	if err != nil {
		return fmt.Sprintf("❌ خطأ في الرابط: %v", err), nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsedURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36 (compatible; NovaBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,text/plain;q=0.8,*/*;q=0.7")
	req.Header.Set("Accept-Language", "ar,en-US;q=0.9,en;q=0.8")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return fmt.Sprintf("❌ تعذر الوصول للموقع: %v", err), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Sprintf("❌ فشل تحميل الصفحة (رمز الاستجابة %d: %s)", resp.StatusCode, resp.Status), nil
	}

	contentType := resp.Header.Get("Content-Type")
	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, w.maxReadBytes))
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	if strings.Contains(contentType, "text/plain") {
		text := string(bodyBytes)
		if len(text) > maxLength {
			text = text[:maxLength] + "\n\n[... تم اقتطاع باقي النص لكبر حجمه ...]"
		}
		return fmt.Sprintf("📄 **محتوى الملف النصي (%s):**\n\n%s", parsedURL.String(), text), nil
	}

	article, err := ExtractHTMLContent(bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("📄 **عنوان الصفحة:** ")
	if article.Title != "" {
		sb.WriteString(article.Title)
	} else {
		sb.WriteString("بدون عنوان")
	}
	sb.WriteString("\n")

	sb.WriteString("🔗 **الرابط:** ")
	sb.WriteString(parsedURL.String())
	sb.WriteString("\n")

	if article.Description != "" {
		sb.WriteString("📝 **الوصف:** ")
		sb.WriteString(article.Description)
		sb.WriteString("\n")
	}

	if article.Author != "" {
		sb.WriteString("✍️ **الكاتب/المصدر:** ")
		sb.WriteString(article.Author)
		sb.WriteString("\n")
	}

	sb.WriteString("\n━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString("📖 **نص المقال والمحتوى:**\n\n")

	body := strings.TrimSpace(article.BodyText)
	if body == "" {
		sb.WriteString("(لم يتم العثور على نص رئيسي قابل للقراءة في الصفحة)")
	} else {
		if len(body) > maxLength {
			sb.WriteString(body[:maxLength])
			sb.WriteString("\n\n[... تم اقتطاع باقي المقال لتجاوز الحد الأقصى للحروف ...]")
		} else {
			sb.WriteString(body)
		}
	}

	return sb.String(), nil
}

// ExtractHTMLContent parses an HTML document and extracts clean text and metadata.
func ExtractHTMLContent(r io.Reader) (*ExtractedArticle, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}

	article := &ExtractedArticle{}
	var bodyBuilder strings.Builder

	var traverse func(*html.Node)
	traverse = func(n *html.Node) {
		if n.Type == html.ElementNode {
			tag := strings.ToLower(n.Data)

			// Skip unwanted tags entirely
			switch tag {
			case "script", "style", "noscript", "nav", "footer", "header", "aside",
				"iframe", "svg", "button", "form", "select", "canvas", "template", "head":
				if tag == "head" {
					// extract metadata from head
					extractHeadMetadata(n, article)
				}
				return
			}

			// Extract metadata if found outside head
			if tag == "title" && article.Title == "" {
				article.Title = extractNodeText(n)
			}
			if tag == "h1" && article.Title == "" {
				article.Title = extractNodeText(n)
			}

			// Block formatting
			switch tag {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				headingText := strings.TrimSpace(extractNodeText(n))
				if headingText != "" {
					prefix := "### "
					if tag == "h1" {
						prefix = "# "
					} else if tag == "h2" {
						prefix = "## "
					}
					bodyBuilder.WriteString("\n\n" + prefix + headingText + "\n\n")
				}
				return

			case "p":
				pText := strings.TrimSpace(extractNodeText(n))
				if pText != "" {
					bodyBuilder.WriteString("\n\n" + pText + "\n\n")
				}
				return

			case "li":
				liText := strings.TrimSpace(extractNodeText(n))
				if liText != "" {
					bodyBuilder.WriteString("• " + liText + "\n")
				}
				return

			case "blockquote":
				quoteText := strings.TrimSpace(extractNodeText(n))
				if quoteText != "" {
					bodyBuilder.WriteString("\n> " + quoteText + "\n\n")
				}
				return

			case "pre", "code":
				codeText := strings.TrimSpace(extractNodeText(n))
				if codeText != "" {
					bodyBuilder.WriteString("\n```\n" + codeText + "\n```\n")
				}
				return
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			traverse(c)
		}
	}

	traverse(doc)

	// Clean up body text whitespace
	article.Title = cleanWhitespace(article.Title)
	article.Description = cleanWhitespace(article.Description)
	article.BodyText = normalizeWhitespace(bodyBuilder.String())

	return article, nil
}

func extractHeadMetadata(head *html.Node, article *ExtractedArticle) {
	for c := head.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode {
			tag := strings.ToLower(c.Data)
			if tag == "title" && article.Title == "" {
				article.Title = extractNodeText(c)
			}
			if tag == "meta" {
				var name, property, content string
				for _, attr := range c.Attr {
					key := strings.ToLower(attr.Key)
					if key == "name" {
						name = strings.ToLower(attr.Val)
					} else if key == "property" {
						property = strings.ToLower(attr.Val)
					} else if key == "content" {
						content = attr.Val
					}
				}

				if (property == "og:title" || name == "twitter:title") && article.Title == "" {
					article.Title = content
				}
				if (name == "description" || property == "og:description") && article.Description == "" {
					article.Description = content
				}
				if (name == "author" || property == "article:author") && article.Author == "" {
					article.Author = content
				}
			}
		}
	}
}

func extractNodeText(n *html.Node) string {
	var sb strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			sb.WriteString(node.Data)
			sb.WriteString(" ")
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			tag := strings.ToLower(c.Data)
			if tag != "script" && tag != "style" && tag != "noscript" {
				walk(c)
			}
		}
	}
	walk(n)
	return sb.String()
}

func cleanWhitespace(s string) string {
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func normalizeWhitespace(s string) string {
	lines := strings.Split(s, "\n")
	var result []string
	consecutiveBlanks := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			consecutiveBlanks++
			if consecutiveBlanks <= 1 {
				result = append(result, "")
			}
		} else {
			consecutiveBlanks = 0
			result = append(result, trimmed)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}
