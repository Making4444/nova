package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	toolsPkg "novabot/internal/tools"
	"novabot/internal/trigger"
)

const (
	openRouterAPIURL = "https://openrouter.ai/api/v1/chat/completions"
	maxFailures      = 3
	maxToolSteps     = 4
)

// TaskScheduler represents an engine that can schedule future autonomous messages.
type TaskScheduler interface {
	ScheduleTask(chatID, chatType, targetUser, reason, durationStr string) string
}

// UserMemoryUpdater represents an interface for saving or updating user facts.
type UserMemoryUpdater interface {
	AppendMemoryNote(userID, userName, note string) error
}

// OpenRouterClient interacts with OpenRouter API with multi-model routing (Chat, Math, Academic, Vision) and fast Groq classification.
type OpenRouterClient struct {
	apiKey              string
	modelChat           string
	modelMath           string
	modelAcademic       string
	modelVision         string
	modelSummarizer     string
	groqRouter          *GroqRouter
	systemPrompt        string
	searchEngine        *SearchEngine
	scheduler           TaskScheduler
	memoryUpdater       UserMemoryUpdater
	httpClient          *http.Client
	mu                  sync.Mutex
	consecutiveFailures int
}

// NewMultiModelClient creates a new client supporting multi-model routing.
func NewMultiModelClient(
	apiKey string,
	modelChat, modelMath, modelAcademic, modelVision, modelSummarizer string,
	groqRouter *GroqRouter,
	systemPrompt string,
) *OpenRouterClient {
	if modelChat == "" {
		modelChat = "openai/gpt-5.6-luna"
	}
	if modelMath == "" {
		modelMath = "nvidia/nemotron-3-super-120b-a12b"
	}
	if modelAcademic == "" {
		modelAcademic = "google/gemini-3.8-flash"
	}
	if modelVision == "" {
		modelVision = "google/gemini-3.8-flash"
	}
	if modelSummarizer == "" {
		modelSummarizer = "deepseek/deepseek-v4-flash-0731"
	}

	return &OpenRouterClient{
		apiKey:          apiKey,
		modelChat:       modelChat,
		modelMath:       modelMath,
		modelAcademic:   modelAcademic,
		modelVision:     modelVision,
		modelSummarizer: modelSummarizer,
		groqRouter:      groqRouter,
		systemPrompt:    systemPrompt,
		searchEngine:    NewSearchEngine(apiKey, "perplexity/sonar"),
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// NewOpenRouterClient creates a backwards-compatible client.
func NewOpenRouterClient(apiKey, model, systemPrompt string) *OpenRouterClient {
	return NewMultiModelClient(apiKey, model, "nvidia/nemotron-3-super-120b-a12b", "google/gemini-3.7-flash", "google/gemma-4-31b-it", "deepseek/deepseek-v4-flash-0731", nil, systemPrompt)
}

// SetScheduler attaches a task scheduler instance to the AI client.
func (c *OpenRouterClient) SetScheduler(scheduler TaskScheduler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scheduler = scheduler
}

// SetMemoryUpdater attaches a memory store instance for updating user profiles.
func (c *OpenRouterClient) SetMemoryUpdater(updater UserMemoryUpdater) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.memoryUpdater = updater
}

// SetGroqRouter attaches the fast query classifier router.
func (c *OpenRouterClient) SetGroqRouter(router *GroqRouter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groqRouter = router
}

type openRouterMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // string or []contentPart
	ToolCallID string      `json:"tool_call_id,omitempty"`
	ToolCalls  []toolCall  `json:"tool_calls,omitempty"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type toolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type toolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type toolDefinition struct {
	Type     string      `json:"type"`
	Function functionDef `json:"function"`
}

type functionDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

var webSearchTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "web_search",
		Description: "ابحث في الإنترنت لحظياً عن أخبار، أسعار، أحداث، حقائق، مواعيد، أو أي معلومات تحتاجها نوفا قبل الرد",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "جملة أو كلمات البحث للبحث عنها في الإنترنت",
				},
			},
			"required": []string{"query"},
		},
	},
}

var scheduleFollowupTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "schedule_followup",
		Description: "جدولة موعد لاحق لتقوم نوفا بالرجوع والتحدث في الشات بعد فترة زمنية معينة (ساعة، ساعتين، بكرة، إلخ)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"duration": map[string]interface{}{
					"type":        "string",
					"description": "المدة الزمنية للرجوع، مثلاً: '1h', '2h', '30m', '1d', 'ساعتين'",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "السبب أو سياق المتابعة (مثلاً: 'اسأل مكاري عمل ايه عند الدكتور')",
				},
				"target_user": map[string]interface{}{
					"type":        "string",
					"description": "اسم أو معرف الشخص المستهدف بالسؤال (اختياري)",
				},
			},
			"required": []string{"duration", "reason"},
		},
	},
}

var getWeatherTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "get_weather",
		Description: "معرفة حالة الطقس المباشرة، درجات الحرارة الحالية والمحسوسة، الرطوبة، سرعة الرياح، وتوقعات الأيام القادمة لأي مدينة أو محافظة في مصر والعالم (مثل: المنيا، القاهرة، الإسكندرية، أسيوط، الجيزة، الرياض، دبي)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"location": map[string]interface{}{
					"type":        "string",
					"description": "اسم المدينة أو المحافظة باللغة العربية أو الإنجليزية (مثلاً: المنيا أو Minya أو القاهرة)",
				},
			},
			"required": []string{"location"},
		},
	},
}

var readWebPageTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "read_web_page",
		Description: "استخراج وقراءة وتلخيص محتوى أي مقال أو صفحة ويب من الرابط (URL)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]interface{}{
					"type":        "string",
					"description": "رابط صفحة الويب للوصول إليها واستخراج المقال منها",
				},
			},
			"required": []string{"url"},
		},
	},
}

var calculatorTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "calculator",
		Description: "إجراء العمليات الحسابية والنسب المئوية والعمليات الحسابية المباشرة بدقة متناهية",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"expression": map[string]interface{}{
					"type":        "string",
					"description": "التعبير الرياضي أو الحسابي (مثال: '1500 * 0.14' أو '2^8 + 50')",
				},
			},
			"required": []string{"expression"},
		},
	},
}

var solveMathProblemTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "solve_math_problem",
		Description: "استدعاء نموذج الرياضيات والاستدلال الخارق (Nemotron Super) لحل أي مسألة رياضية أو معادلة أو تفاضل وتكامل أو هندسة أو لوجيك وبرمجة بدقة متناهية",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"problem": map[string]interface{}{
					"type":        "string",
					"description": "نص المسألة الرياضية أو المعادلة بالتفصيل",
				},
			},
			"required": []string{"problem"},
		},
	},
}

var consultCurriculumTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "consult_curriculum_expert",
		Description: "استدعاء واستشارة خبير المناهج الدراسية والعلوم واللغات الشامل (Gemini 3.8 Flash) لحل وشرح وتفصيل أي أسئلة أو تدريبات أو امتحانات تخص المناهج والمواد الدراسية (فيزياء، كيمياء، أحياء، لغة عربية وقواعد ونحو وبلاغة، إنجليزي، لغات، تاريخ، جغرافيا، فلسفة وعلم نفس، مواد جامعية، إلخ) بدقة علمية وتربوية فائقة",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"subject": map[string]interface{}{
					"type":        "string",
					"description": "اسم المادة أو الفرع الدراسي (مثلاً: لغة إنجليزية، كيمياء، نحو وبلاغة، فيزياء، أحياء، تاريخ، دراسات)",
				},
				"question": map[string]interface{}{
					"type":        "string",
					"description": "نص السؤال أو التدريب أو القاعدة أو القطعة الدراسية المطلوب حلها وشرحها بالتفصيل",
				},
			},
			"required": []string{"subject", "question"},
		},
	},
}

var updateUserMemoryTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "update_user_memory",
		Description: "حفظ أو تحديث معلومة مهمة عن المستخدم في ملف ذاكرته الدائمة",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "معرف المستخدم (sender_id)",
				},
				"user_name": map[string]interface{}{
					"type":        "string",
					"description": "اسم المستخدم",
				},
				"note": map[string]interface{}{
					"type":        "string",
					"description": "المعلومة أو الملاحظة الجديدة لحفظها",
				},
			},
			"required": []string{"note"},
		},
	},
}

var sendVoiceNoteTool = toolDefinition{
	Type: "function",
	Function: functionDef{
		Name:        "send_voice_note",
		Description: "إرسال الرد كرسالة صوتية (WhatsApp Voice Note) بصوتك البشري الواقعي ونبرتك المصرية الحية، عندما يطلب المستخدم نطق أو سماع كلام ('انطق'، 'قول ده'، 'اقرأ ده'، 'سمعني')، أو عندما تختار الرد بصوتك. يُسمح حصرياً هنا باستخدام وسوم التعبير الصوتي الحية مثل [laughs]، [chuckles]، [sighs]، [whispers]، [gasp]، [pause]",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"speech_text": map[string]interface{}{
					"type":        "string",
					"description": "النص الصوتي المراد تسجيله ونطقه بصوتك بالعامية المصرية شاملاً وسوم التعبير الصوتي الطبيعية",
				},
			},
			"required": []string{"speech_text"},
		},
	},
}

type openRouterRequest struct {
	Model     string              `json:"model"`
	Messages  []openRouterMessage `json:"messages"`
	Tools     []toolDefinition    `json:"tools,omitempty"`
	MaxTokens int                 `json:"max_tokens,omitempty"`
}

type openRouterChoice struct {
	Message openRouterMessage `json:"message"`
}

type openRouterResponse struct {
	Choices []openRouterChoice `json:"choices"`
	Error   *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// GenerateResponse sends the payload to OpenRouter using multi-model smart routing.
func (c *OpenRouterClient) GenerateResponse(ctx context.Context, payload *trigger.RequestPayload) (*ResponsePayload, error) {
	if c == nil {
		return nil, errors.New("OpenRouterClient is nil")
	}
	if c.apiKey == "" {
		return nil, errors.New("OPENROUTER_API_KEY is not set")
	}
	if payload == nil {
		return nil, errors.New("cannot generate response for nil payload")
	}

	// 1. Smart Model Selection
	selectedModel := c.modelChat
	mediaURL := payload.MediaDataURL

	if mediaURL != nil && *mediaURL != "" {
		selectedModel = c.modelVision
		fmt.Printf("\n[🧠 Smart Router] Image attached -> Routed to Vision Model: %s\n", selectedModel)
	} else if c.groqRouter != nil {
		category := c.groqRouter.ClassifyQuery(ctx, payload.MessageText)
		if category == CategoryMath {
			selectedModel = c.modelMath
			fmt.Printf("\n[🧠 Smart Router] Math problem detected by Groq -> Routed to Math Specialist: %s\n", selectedModel)
		} else {
			selectedModel = c.modelChat
			fmt.Printf("\n[🧠 Smart Router] Conversational query -> Routed to Main Chat Model: %s\n", selectedModel)
		}
	}

	// Prepare user message content (Multimodal if MediaDataURL exists)
	var userContent interface{}
	payloadForText := *payload
	payloadForText.MediaDataURL = nil
	payloadBytes, err := json.Marshal(payloadForText)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	if mediaURL != nil && *mediaURL != "" {
		userContent = []contentPart{
			{
				Type: "text",
				Text: string(payloadBytes),
			},
			{
				Type: "image_url",
				ImageURL: &imageURL{
					URL: *mediaURL,
				},
			},
		}
	} else {
		userContent = string(payloadBytes)
	}

	messages := []openRouterMessage{
		{Role: "system", Content: c.systemPrompt},
		{Role: "user", Content: userContent},
	}

	// 2. Execute initial attempt with Tool Calling loop
	rawResp, usedSearch, err := c.executeConversation(ctx, selectedModel, messages, payload)
	if err != nil {
		c.recordFailure()
		return nil, fmt.Errorf("openrouter request failed: %w", err)
	}

	parsed, parseErr := ParseResponse(rawResp)
	if parseErr == nil {
		c.recordSuccess()
		if usedSearch && parsed.ReplyText != nil && *parsed.ReplyText != "" {
			trimmed := strings.TrimSpace(*parsed.ReplyText)
			modified := "تم البحث\n\n" + trimmed
			parsed.ReplyText = &modified
		}
		return parsed, nil
	}

	// 3. Single retry with correction prompt if JSON parsing fails
	retryMessages := append(messages,
		openRouterMessage{Role: "assistant", Content: rawResp},
		openRouterMessage{
			Role:    "user",
			Content: "Your previous response was not valid JSON matching the schema. Please return ONLY a valid raw JSON object matching the required schema with no markdown code fences or explanations.",
		},
	)

	rawRetryResp, retryUsedSearch, retryErr := c.executeConversation(ctx, selectedModel, retryMessages, payload)
	if retryErr != nil {
		c.recordFailure()
		return nil, fmt.Errorf("retry openrouter request failed: %w", retryErr)
	}

	parsedRetry, retryParseErr := ParseResponse(rawRetryResp)
	if retryParseErr != nil {
		c.recordFailure()
		return nil, fmt.Errorf("failed to parse AI response after retry (%w): initial raw: %q, retry raw: %q", retryParseErr, rawResp, rawRetryResp)
	}

	c.recordSuccess()
	if (usedSearch || retryUsedSearch) && parsedRetry.ReplyText != nil && *parsedRetry.ReplyText != "" {
		trimmed := strings.TrimSpace(*parsedRetry.ReplyText)
		modified := "تم البحث\n\n" + trimmed
		parsedRetry.ReplyText = &modified
	}

	return parsedRetry, nil
}

// SolveMathDirectly invokes the specialized math/reasoning model directly to solve mathematical questions.
func (c *OpenRouterClient) SolveMathDirectly(ctx context.Context, mathProblem string) (string, error) {
	if c == nil {
		return "", errors.New("OpenRouterClient is nil")
	}
	mathModel := c.modelMath
	if mathModel == "" {
		mathModel = "nvidia/nemotron-3-super"
	}
	messages := []openRouterMessage{
		{
			Role:    "system",
			Content: "You are an expert mathematical, logical, and scientific reasoning engine. Solve the provided problem step-by-step with extreme accuracy, clarity, and precision. Provide complete mathematical derivations, answers, and concise explanations in Arabic.",
		},
		{
			Role:    "user",
			Content: mathProblem,
		},
	}

	choiceMsg, err := c.callAPI(ctx, mathModel, messages, nil)
	if err != nil {
		return "", err
	}

	if contentStr, ok := choiceMsg.Content.(string); ok {
		return strings.TrimSpace(contentStr), nil
	}
	return "", fmt.Errorf("invalid math response format")
}

// SolveAcademicDirectly delegates academic, school/college curricula, science, language, and humanities questions directly to the high-IQ academic model.
func (c *OpenRouterClient) SolveAcademicDirectly(ctx context.Context, subject, query string) (string, error) {
	targetModel := c.modelAcademic
	if targetModel == "" {
		targetModel = "google/gemini-3.7-flash"
	}

	academicPrompt := fmt.Sprintf(`أنت خبير ومستشار المناهج الدراسية والعلوم واللغات الشامل عالي الذكاء (Academic & Curriculum Specialist).
المادة/التخصص: %s
المسألة أو السؤال:
%s

قم بحل وشرح السؤال بالتفصيل وبأعلى درجات الدقة الأكاديمية والتربوية، مع توضيح خطوات الإجابة والقواعد العلمية أو اللغوية بأسلوب مبسط ومنظم.`, subject, query)

	messages := []openRouterMessage{
		{
			Role:    "user",
			Content: academicPrompt,
		},
	}

	choiceMsg, err := c.callAPICustom(ctx, targetModel, messages, nil, 3000)
	if err != nil {
		return "", err
	}

	if contentStr, ok := choiceMsg.Content.(string); ok {
		return strings.TrimSpace(contentStr), nil
	}
	return "", fmt.Errorf("invalid academic response format")
}

// executeConversation executes the message list, handling tool calls (web_search, schedule_followup, solve_math_problem, consult_curriculum_expert, update_user_memory).
func (c *OpenRouterClient) executeConversation(ctx context.Context, model string, messages []openRouterMessage, payload *trigger.RequestPayload) (string, bool, error) {
	currentMessages := make([]openRouterMessage, len(messages))
	copy(currentMessages, messages)
	usedSearch := false

	tools := []toolDefinition{webSearchTool, getWeatherTool, readWebPageTool, calculatorTool, scheduleFollowupTool, solveMathProblemTool, consultCurriculumTool, updateUserMemoryTool, sendVoiceNoteTool}

	for step := 0; step < maxToolSteps; step++ {
		choiceMsg, err := c.callAPI(ctx, model, currentMessages, tools)
		if err != nil {
			return "", usedSearch, err
		}

		if len(choiceMsg.ToolCalls) == 0 {
			if contentStr, ok := choiceMsg.Content.(string); ok {
				return strings.TrimSpace(contentStr), usedSearch, nil
			}
			return "", usedSearch, fmt.Errorf("unexpected content format in model response")
		}

		currentMessages = append(currentMessages, choiceMsg)

		for _, call := range choiceMsg.ToolCalls {
			switch call.Function.Name {
			case "web_search":
				usedSearch = true
				var args struct {
					Query string `json:"query"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				query := strings.TrimSpace(args.Query)
				if query == "" {
					query = call.Function.Arguments
				}

				fmt.Printf("\n[🔍 Web Search] Query: %q\n", query)
				searchResult, searchErr := c.searchEngine.Search(ctx, query)
				if searchErr != nil {
					searchResult = fmt.Sprintf("فشل البحث على الإنترنت: %v", searchErr)
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    searchResult,
				})

			case "get_weather", "weather":
				var args struct {
					Location string `json:"location"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				loc := strings.TrimSpace(args.Location)
				if loc == "" {
					loc = call.Function.Arguments
				}
				fmt.Printf("\n[☀️ Weather Tool] Fetching live weather for: %q\n", loc)
				weatherTool := toolsPkg.NewWeatherTool()
				weatherRes, wErr := weatherTool.Execute(ctx, []byte(call.Function.Arguments), toolsPkg.ExecutionContext{})
				toolResult := weatherRes
				if wErr != nil {
					toolResult = fmt.Sprintf("تعذر جلب بيانات الطقس: %v", wErr)
				}
				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    toolResult,
				})

			case "read_web_page", "web_reader":
				var args struct {
					URL string `json:"url"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				u := strings.TrimSpace(args.URL)
				if u == "" {
					u = call.Function.Arguments
				}
				fmt.Printf("\n[📄 Web Reader Tool] Reading URL: %q\n", u)
				readerTool := toolsPkg.NewWebReaderTool()
				readerRes, rErr := readerTool.Execute(ctx, []byte(call.Function.Arguments), toolsPkg.ExecutionContext{})
				toolResult := readerRes
				if rErr != nil {
					toolResult = fmt.Sprintf("تعذر قراءة الصفحة: %v", rErr)
				}
				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    toolResult,
				})

			case "calculator":
				var args struct {
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				expr := strings.TrimSpace(args.Expression)
				if expr == "" {
					expr = call.Function.Arguments
				}
				fmt.Printf("\n[🧮 Calculator Tool] Evaluating expression: %q\n", expr)
				calcTool := toolsPkg.NewCalculatorTool()
				calcRes, cErr := calcTool.Execute(ctx, []byte(call.Function.Arguments), toolsPkg.ExecutionContext{})
				toolResult := calcRes
				if cErr != nil {
					toolResult = fmt.Sprintf("تعذر حساب التعبير: %v", cErr)
				}
				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    toolResult,
				})

			case "solve_math_problem":
				var args struct {
					Problem string `json:"problem"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				targetMathModel := c.modelMath
				if targetMathModel == "" {
					targetMathModel = "nvidia/nemotron-3-super-120b-a12b"
				}
				fmt.Printf("\n[🧮 Math Consultant] Consulting %s on problem: %q\n", targetMathModel, args.Problem)

				mathSolution, mathErr := c.SolveMathDirectly(ctx, args.Problem)
				if mathErr != nil {
					mathSolution = fmt.Sprintf("فشل حل المسألة الرياضية: %v", mathErr)
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    mathSolution,
				})

			case "consult_curriculum_expert", "curriculum_expert", "academic_solver":
				var args struct {
					Subject  string `json:"subject"`
					Question string `json:"question"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				sub := strings.TrimSpace(args.Subject)
				q := strings.TrimSpace(args.Question)
				if q == "" {
					q = call.Function.Arguments
				}
				if sub == "" {
					sub = "مناهج دراسية عامة"
				}
				targetAcademicModel := c.modelAcademic
				if targetAcademicModel == "" {
					targetAcademicModel = "google/gemini-3.7-flash"
				}
				fmt.Printf("\n[🎓 Curriculum Consultant] Consulting %s on subject (%s): %q\n", targetAcademicModel, sub, q)

				acadSolution, acadErr := c.SolveAcademicDirectly(ctx, sub, q)
				if acadErr != nil {
					acadSolution = fmt.Sprintf("فشل استشارة خبير المناهج: %v", acadErr)
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    acadSolution,
				})

			case "schedule_followup":
				var args struct {
					Duration   string `json:"duration"`
					Reason     string `json:"reason"`
					TargetUser string `json:"target_user"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)

				toolResult := "تم تسجيل المتابعة المجدولة في السيرفر بنجاح وسيتم تذكيرك بها."
				c.mu.Lock()
				sch := c.scheduler
				c.mu.Unlock()

				if sch != nil && payload != nil {
					target := args.TargetUser
					if target == "" {
						target = payload.SenderName
					}
					taskID := sch.ScheduleTask(payload.ChatID, payload.ChatType, target, args.Reason, args.Duration)
					toolResult = fmt.Sprintf("تمت جدولة المهمة برقم %s للمتابعة بعد %s", taskID, args.Duration)
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    toolResult,
				})

			case "update_user_memory":
				var args struct {
					UserID   string `json:"user_id"`
					UserName string `json:"user_name"`
					Note     string `json:"note"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)

				uID := args.UserID
				if uID == "" && payload != nil {
					uID = payload.SenderID
				}
				uName := args.UserName
				if uName == "" && payload != nil {
					uName = payload.SenderName
				}

				toolResult := "تم حفظ الملاحظة في ذاكرة المستخدم بنجاح."
				c.mu.Lock()
				mem := c.memoryUpdater
				c.mu.Unlock()

				if mem != nil && uID != "" && args.Note != "" {
					_ = mem.AppendMemoryNote(uID, uName, args.Note)
				}

				currentMessages = append(currentMessages, openRouterMessage{
					Role:       "tool",
					ToolCallID: call.ID,
					Content:    toolResult,
				})

			case "send_voice_note", "voice_note":
				var args struct {
					SpeechText string `json:"speech_text"`
				}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)
				speechText := strings.TrimSpace(args.SpeechText)
				if speechText == "" {
					speechText = call.Function.Arguments
				}
				fmt.Printf("\n[🎙️ Voice Note Tool] AI executed send_voice_note: %q\n", speechText)
				voicePayload := map[string]interface{}{
					"should_reply":  true,
					"reply_text":    speechText,
					"send_as_voice": true,
				}
				vBytes, _ := json.Marshal(voicePayload)
				return string(vBytes), usedSearch, nil
			}
		}
	}

	return "", usedSearch, fmt.Errorf("exceeded maximum tool calling steps")
}

func (c *OpenRouterClient) callAPI(ctx context.Context, model string, messages []openRouterMessage, tools []toolDefinition) (openRouterMessage, error) {
	return c.callAPICustom(ctx, model, messages, tools, 2000)
}

func (c *OpenRouterClient) callAPICustom(ctx context.Context, model string, messages []openRouterMessage, tools []toolDefinition, maxTokens int) (openRouterMessage, error) {
	if maxTokens <= 0 {
		maxTokens = 2000
	}
	reqBody := openRouterRequest{
		Model:     model,
		Messages:  messages,
		Tools:     tools,
		MaxTokens: maxTokens,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return openRouterMessage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return openRouterMessage{}, err
	}

	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova WhatsApp Bot")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return openRouterMessage{}, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return openRouterMessage{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return openRouterMessage{}, fmt.Errorf("openrouter API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		return openRouterMessage{}, fmt.Errorf("failed to decode openrouter response: %w", err)
	}

	if orResp.Error != nil {
		return openRouterMessage{}, fmt.Errorf("openrouter returned error: %s (code: %d)", orResp.Error.Message, orResp.Error.Code)
	}

	if len(orResp.Choices) == 0 {
		return openRouterMessage{}, errors.New("openrouter returned empty choices list")
	}

	return orResp.Choices[0].Message, nil
}

// SummarizeChatHistory analyzes raw messages from a conversation and generates a structured summary + user profiles.
func (c *OpenRouterClient) SummarizeChatHistory(ctx context.Context, messagesText string) (string, map[string]string, error) {
	if c == nil {
		return "", nil, errors.New("OpenRouterClient is nil")
	}
	if c.apiKey == "" {
		return "", nil, errors.New("OPENROUTER_API_KEY is not set")
	}

	// Optimize very large transcripts to prevent timeout while keeping full historical context
	if len(messagesText) > 250000 {
		head := messagesText[:60000]
		tail := messagesText[len(messagesText)-140000:]
		messagesText = head + "\n\n[... تم اقتطاع جزء من منتصف المحادثة للحفاظ على سرعة المعالجة ...]\n\n" + tail
	}

	systemPrompt := `أنت محلل محادثات ذكي ومساعد لبوت نوفا. مهمتك تحليل سجل المحادثة الكامل وتحويله إلى:
1. "summary": ملخص شامل وممتع للمحادثة بصيغة Markdown، يبرز أهم المواضيع، النكات، الأحداث، والقرارات المتخذة.
2. "users": قاموس يحتوي على بطاقة تعريفية لكل مستخدم شارك في الشات (المفتاح هو user_id أو اسمه، والقيمة هي ملخص شخصيته واهتماماته ومعلومات عنه).

أرجع النتيجة بصيغة JSON نظيفة فقط:
{
  "summary": "نص الملخص الشامل للمحادثة بصيغة Markdown",
  "users": {
    "user_id_or_name": "معلومات وبطاقة المستخدم"
  }
}`

	reqMessages := []openRouterMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: messagesText},
	}

	summarizerModel := c.modelSummarizer
	if summarizerModel == "" {
		summarizerModel = "google/gemini-2.5-flash"
	}

	fmt.Printf("\n[📋 Summarizer] Sending chat transcript (%d chars) to long-context model: %s\n", len(messagesText), summarizerModel)
	choiceMsg, err := c.callAPICustom(ctx, summarizerModel, reqMessages, nil, 4000)
	if err != nil {
		fmt.Printf("[⚠️ Summarizer Warning] %s failed (%v). Retrying with fallback google/gemini-2.5-flash...\n", summarizerModel, err)
		choiceMsg, err = c.callAPICustom(ctx, "google/gemini-2.5-flash", reqMessages, nil, 4000)
		if err != nil {
			fmt.Printf("[⚠️ Summarizer Warning] google/gemini-2.5-flash failed (%v). Retrying with google/gemini-2.0-flash-001...\n", err)
			choiceMsg, err = c.callAPICustom(ctx, "google/gemini-2.0-flash-001", reqMessages, nil, 4000)
			if err != nil {
				return "", nil, fmt.Errorf("failed to summarize chat transcript with AI models: %w", err)
			}
		}
	}

	contentStr, ok := choiceMsg.Content.(string)
	if !ok {
		return "", nil, fmt.Errorf("unexpected summary response format")
	}

	jsonStr := ExtractJSON(contentStr)
	var result struct {
		Summary string            `json:"summary"`
		Users   map[string]string `json:"users"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		// Fallback: use raw text as summary
		return contentStr, nil, nil
	}

	return result.Summary, result.Users, nil
}

func (c *OpenRouterClient) recordSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures = 0
}

func (c *OpenRouterClient) recordFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consecutiveFailures++
}

