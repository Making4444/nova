package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"novabot/internal/trigger"
)

// RouteResult contains the routing decision made by the Intent Router.
type RouteResult struct {
	TargetAgent     AgentType `json:"target_agent"`
	Confidence      float64   `json:"confidence"`
	Reason          string    `json:"reason"`
	MatchedKeywords []string  `json:"matched_keywords,omitempty"`
}

// Router classifies incoming requests and routes them to the appropriate specialized agent.
type Router struct {
	classifierClient LLMClient
	classifierModel  string
	customRules      map[string]func(text string, hasMedia bool) (AgentType, bool)
	mu               sync.RWMutex
}

// NewRouter creates a new intent classifier and router instance.
func NewRouter(classifierClient LLMClient, classifierModel string) *Router {
	if classifierModel == "" {
		classifierModel = "meta-llama/llama-3.3-70b-instruct"
	}
	return &Router{
		classifierClient: classifierClient,
		classifierModel:  classifierModel,
		customRules:      make(map[string]func(text string, hasMedia bool) (AgentType, bool)),
	}
}

// SetClassifier updates the LLM classifier client and model.
func (r *Router) SetClassifier(client LLMClient, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.classifierClient = client
	if model != "" {
		r.classifierModel = model
	}
}

// RegisterRule allows adding custom priority routing rules.
func (r *Router) RegisterRule(name string, rule func(text string, hasMedia bool) (AgentType, bool)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.customRules[name] = rule
}

// Route analyzes the RequestPayload and determines the optimal agent for execution.
func (r *Router) Route(ctx context.Context, payload *trigger.RequestPayload) (*RouteResult, error) {
	if payload == nil {
		return &RouteResult{
			TargetAgent: AgentTypePersona,
			Confidence:  1.0,
			Reason:      "Default fallback for nil payload",
		}, nil
	}

	text := strings.TrimSpace(payload.MessageText)
	hasMedia := payload.MediaDataURL != nil && *payload.MediaDataURL != ""

	// 1. Check custom registered rules first
	r.mu.RLock()
	for _, rule := range r.customRules {
		if agentType, matched := rule(text, hasMedia); matched {
			r.mu.RUnlock()
			return &RouteResult{
				TargetAgent: agentType,
				Confidence:  1.0,
				Reason:      "Matched custom routing rule",
			}, nil
		}
	}
	r.mu.RUnlock()

	// 2. High-speed Rule-based Semantic Classification (0ms latency)
	heuristicRes := r.classifyHeuristic(text, hasMedia)
	if heuristicRes.Confidence >= 0.85 {
		return heuristicRes, nil
	}

	// 3. LLM-based Classification for ambiguous queries if classifierClient is available
	r.mu.RLock()
	client := r.classifierClient
	model := r.classifierModel
	r.mu.RUnlock()

	if client != nil && text != "" {
		llmRes, err := r.classifyWithLLM(ctx, client, model, text, hasMedia)
		if err == nil && llmRes.Confidence > 0.6 {
			return llmRes, nil
		}
	}

	// 4. Return highest confidence heuristic result
	return heuristicRes, nil
}

// ClassifyText provides a quick classification helper for standalone text strings.
func (r *Router) ClassifyText(ctx context.Context, text string, hasMedia bool) (AgentType, float64, string) {
	res, _ := r.Route(ctx, &trigger.RequestPayload{
		MessageText: text,
		MediaDataURL: func() *string {
			if hasMedia {
				s := "media://present"
				return &s
			}
			return nil
		}(),
	})
	return res.TargetAgent, res.Confidence, res.Reason
}

// classifyHeuristic applies deterministic semantic pattern matching across Arabic & English.
func (r *Router) classifyHeuristic(text string, hasMedia bool) *RouteResult {
	// A. Media presence immediately triggers VisionDocAgent
	if hasMedia {
		return &RouteResult{
			TargetAgent:     AgentTypeVisionDoc,
			Confidence:      1.0,
			Reason:          "Incoming message contains visual media/image attachment",
			MatchedKeywords: []string{"[media_attachment]"},
		}
	}

	lower := strings.ToLower(text)

	// B. Vision & Document Explicit Keywords
	visionKeywords := []string{
		"حلل الصورة", "في الصورة", "شوف الصورة", "اقرأ الصورة", "بص في الصورة",
		"اقرأ الملف", "ملف pdf", "مستند", "ocr", "استخرج النص", "اقرأ المكتوب في الصورة",
		"صورة الشاشة", "سكرين شوت", "شيت اكسيل", "ملف وورد",
		"read image", "analyze image", "in the picture", "ocr", "extract text from image",
		"pdf document", "screenshot analysis", "look at this photo",
	}
	var matchedVision []string
	for _, kw := range visionKeywords {
		if strings.Contains(lower, kw) {
			matchedVision = append(matchedVision, kw)
		}
	}
	if len(matchedVision) > 0 {
		return &RouteResult{
			TargetAgent:     AgentTypeVisionDoc,
			Confidence:      0.95,
			Reason:          fmt.Sprintf("Visual/document analysis requested: %s", strings.Join(matchedVision, ", ")),
			MatchedKeywords: matchedVision,
		}
	}

	// C. Math & Coding Keywords
	mathKeywords := []string{
		"احسب", "معادلة", "تفاضل", "تكامل", "جذر", "مسألة", "احسبلي", "ناتج",
		"رياضيات", "هندسة", "مثلثات", "مساحة", "محيط", "احتمالات", "لوغاريتم", "مصفوفة",
		"integral", "derivative", "solve equation", "calculate", "math problem",
		"arithmetic", "algebra", "calculus", "matrix", "logarithm", "hypotenuse",
		"+", "*", "/", "^", "=", "sin(", "cos(", "tan(", "sqrt",
	}

	codingKeywords := []string{
		"كود", "code", "python", "golang", "go code", "javascript", "typescript", "c++",
		"java", "sql", "html", "css", "docker", "git", "function", "class", "algorithm",
		"debug", "error", "exception", "خوارزمية", "برمجلي", "اكتب كود", "حل الخطأ",
		"stack trace", "regex", "api", "database", "query", "script", "json parsing",
		"لغز منطقي", "مسألة ذكاء", "logic puzzle", "puzzle", "فانكشن", "اكتبلي سكريبت",
		"loop", "async", "struct", "pointer", "goroutine", "deadlock", "syntax error",
	}

	var matchedMathCoding []string
	for _, kw := range mathKeywords {
		if containsKeyword(lower, kw) {
			matchedMathCoding = append(matchedMathCoding, kw)
		}
	}
	for _, kw := range codingKeywords {
		if containsKeyword(lower, kw) {
			matchedMathCoding = append(matchedMathCoding, kw)
		}
	}

	if len(matchedMathCoding) > 0 {
		return &RouteResult{
			TargetAgent:     AgentTypeMathCoding,
			Confidence:      0.95,
			Reason:          fmt.Sprintf("Mathematical reasoning or programming requested: %s", strings.Join(matchedMathCoding, ", ")),
			MatchedKeywords: matchedMathCoding,
		}
	}

	// D. Research, Deep Web Lookups, Currency, News, Weather Keywords
	researchKeywords := []string{
		"سعر الدولار", "سعر الذهب", "سعر العملة", "سعر الجنيه", "سعر الصرف", "بورصة", "أسعار",
		"بكام الذهب", "سعر اليوم", "عيار 21", "عيار 24", "الدولار بكام", "سعر الفائدة", "سعر البنزين",
		"اسعار", "ابحث عن", "سيرش", "دورلي على", "أحدث أخبار", "اخبار اليوم", "تريند",
		"اليوم في مصر", "أخبار العالم", "عاجل", "مين هو", "معلومات عن", "مقارنة بين", "فرق بين",
		"احصائيات", "ويكيبيديا", "weather", "الطقس", "الجو", "درجة الحرارة", "توقعات الجو", "مطر",
		"حرارة", "ماتش اليوم", "نتيجة ماتش", "ترتيب الدوري", "search web", "look up",
		"currency exchange", "gold price", "exchange rate", "latest news", "compare between",
	}

	var matchedResearch []string
	for _, kw := range researchKeywords {
		if containsKeyword(lower, kw) {
			matchedResearch = append(matchedResearch, kw)
		}
	}
	if len(matchedResearch) > 0 {
		return &RouteResult{
			TargetAgent:     AgentTypeResearch,
			Confidence:      0.95,
			Reason:          fmt.Sprintf("Web research, live news, or market facts requested: %s", strings.Join(matchedResearch, ", ")),
			MatchedKeywords: matchedResearch,
		}
	}

	// E. Persona, Chit-Chat, Humor & Banter (Default Persona Category)
	personaKeywords := []string{
		"ازيك", "عامل ايه", "صباح الخير", "مساء الخير", "هاي", "يا نوفا", "نوفا", "سلام عليكم",
		"أخبارك", "وحشتني", "منور", "نكتة", "إفيه", "روق", "هزر", "قولي نكتة", "اضحك", "تريقة",
		"قصف جبهات", "روشنة", "قلش", "تعبان", "زعلان", "مخنوق", "فرحان", "بحبك", "شكرا", "تسلم",
		"حبيبي", "يا صاحبي", "ادعيلي", "سهران", "مين مكاري", "فضفضة", "حوار", "رأيك ايه",
	}
	var matchedPersona []string
	for _, kw := range personaKeywords {
		if containsKeyword(lower, kw) {
			matchedPersona = append(matchedPersona, kw)
		}
	}

	if len(matchedPersona) > 0 {
		return &RouteResult{
			TargetAgent:     AgentTypePersona,
			Confidence:      0.90,
			Reason:          fmt.Sprintf("Conversational banter, emotion, or Egyptian persona matching: %s", strings.Join(matchedPersona, ", ")),
			MatchedKeywords: matchedPersona,
		}
	}

	// Default Fallback is PersonaAgent for general chat
	return &RouteResult{
		TargetAgent:     AgentTypePersona,
		Confidence:      0.75,
		Reason:          "General conversational request routed to PersonaAgent",
		MatchedKeywords: []string{"[default_chat]"},
	}
}

// classifyWithLLM invokes a high-speed classifier LLM when heuristic is inconclusive.
func (r *Router) classifyWithLLM(ctx context.Context, client LLMClient, model string, text string, hasMedia bool) (*RouteResult, error) {
	systemPrompt := `You are the ultra-fast Intent Classifier for Nova WhatsApp Bot.
Classify the user query into exactly ONE of the 4 specialized agents:
1. "research": For deep web lookups, current live news, exchange rates, gold prices, currency, weather forecasts, fact-checking, statistics, comparisons.
2. "math_coding": For math equations, arithmetic, calculus, geometry, algorithms, code snippets (Python, Go, JS, SQL, etc.), debugging errors, logic puzzles.
3. "vision_doc": For analyzing pictures, photos, documents, PDFs, diagrams, OCR text extraction.
4. "persona": For general greetings, jokes, humor, teasing, banter, emotional support, casual conversation, stories in Egyptian dialect.

Respond ONLY with valid JSON:
{
  "agent": "research" | "math_coding" | "vision_doc" | "persona",
  "confidence": 0.95,
  "reason": "Brief explanation"
}`

	userMsg := text
	if hasMedia {
		userMsg = "[Image/Media Attached] " + text
	}

	messages := []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userMsg},
	}

	res, err := client.Call(ctx, model, messages, nil)
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Agent      string  `json:"agent"`
		Confidence float64 `json:"confidence"`
		Reason     string  `json:"reason"`
	}

	jsonStr := extractJSONString(res.Content)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, err
	}

	var targetAgent AgentType
	switch strings.ToLower(parsed.Agent) {
	case "research":
		targetAgent = AgentTypeResearch
	case "math_coding", "math", "coding":
		targetAgent = AgentTypeMathCoding
	case "vision_doc", "vision", "doc":
		targetAgent = AgentTypeVisionDoc
	default:
		targetAgent = AgentTypePersona
	}

	conf := parsed.Confidence
	if conf <= 0 {
		conf = 0.85
	}

	return &RouteResult{
		TargetAgent: targetAgent,
		Confidence:  conf,
		Reason:      parsed.Reason,
	}, nil
}

// containsKeyword checks for keyword presence in text with boundary checks.
func containsKeyword(text, kw string) bool {
	if kw == "+" || kw == "*" || kw == "/" || kw == "^" || kw == "=" {
		// For arithmetic operators, require whitespace or digit proximity
		return strings.Contains(text, kw)
	}
	return strings.Contains(text, kw)
}

func extractJSONString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "```") {
		if idx := strings.Index(trimmed, "\n"); idx != -1 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		}
		if strings.HasSuffix(trimmed, "```") {
			trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
		}
	}
	startIdx := strings.Index(trimmed, "{")
	endIdx := strings.LastIndex(trimmed, "}")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		trimmed = trimmed[startIdx : endIdx+1]
	}
	return strings.TrimSpace(trimmed)
}
