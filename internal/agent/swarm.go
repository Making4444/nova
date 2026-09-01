package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"novabot/internal/ai"
	"novabot/internal/emotion"
	"novabot/internal/tools"
	"novabot/internal/trigger"
)

// Swarm orchestrates the multi-agent system, integrates the emotion engine, and compiles responses in authentic Egyptian dialect.
type Swarm struct {
	router        *Router
	agents        map[AgentType]Agent
	emotionEngine *emotion.Engine
	toolsRegistry *tools.Registry
	llmClient     LLMClient
	config        *SwarmConfig
	systemPrompt  string
	makerID       string
	mu            sync.RWMutex
}

// NewSwarm initializes a fully-equipped Swarm orchestrator.
func NewSwarm(
	cfg *SwarmConfig,
	emotionEngine *emotion.Engine,
	toolsRegistry *tools.Registry,
	llmClient LLMClient,
) *Swarm {
	if cfg == nil {
		cfg = DefaultSwarmConfig()
	}

	router := NewRouter(llmClient, cfg.ModelRouter)

	swarm := &Swarm{
		router:        router,
		agents:        make(map[AgentType]Agent),
		emotionEngine: emotionEngine,
		toolsRegistry: toolsRegistry,
		llmClient:     llmClient,
		config:        cfg,
		systemPrompt:  cfg.SystemPrompt,
		makerID:       "201202172699",
	}

	// Register default specialized agents
	swarm.RegisterAgent(NewResearchAgent(llmClient, cfg.ModelResearch, toolsRegistry))
	swarm.RegisterAgent(NewMathCodingAgent(llmClient, cfg.ModelMathCoding, toolsRegistry))
	swarm.RegisterAgent(NewVisionDocAgent(llmClient, cfg.ModelVisionDoc))
	swarm.RegisterAgent(NewPersonaAgent(llmClient, cfg.ModelPersona))

	return swarm
}

// SetMakerID sets the phone number or ID of Nova's maker (Making/Makari).
func (s *Swarm) SetMakerID(makerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.makerID = makerID
}

// SetSystemPrompt updates the base system constitution.
func (s *Swarm) SetSystemPrompt(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.systemPrompt = prompt
	s.config.SystemPrompt = prompt
}

// RegisterAgent registers a specialized agent.
func (s *Swarm) RegisterAgent(agent Agent) {
	if agent == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[agent.Type()] = agent
}

// GetAgent returns a registered agent by its type.
func (s *Swarm) GetAgent(agentType AgentType) (Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.agents[agentType]
	return agent, ok
}

// Router returns the underlying intent router.
func (s *Swarm) Router() *Router {
	return s.router
}

// Process orchestrates the end-to-end multi-agent execution pipeline.
func (s *Swarm) Process(ctx context.Context, payload *trigger.RequestPayload) (*ai.ResponsePayload, error) {
	if payload == nil {
		return nil, fmt.Errorf("cannot process nil request payload")
	}

	// 1. Determine Maker status
	isMaker := s.isMakerUser(payload.SenderID, payload.SenderName)

	// 2. Emotion Engine Integration
	var emoState *emotion.EmotionalState
	var emoPrompt string
	if s.emotionEngine != nil {
		emoState = s.emotionEngine.UpdateMood(payload.ChatID, payload.SenderID, payload.SenderName, payload.MessageText, isMaker, "")
		emoPrompt = s.emotionEngine.BuildPromptContext(payload.ChatID, payload.SenderID, payload.SenderName, isMaker)
	}

	// 3. Intent Classification & Routing
	routeResult, err := s.router.Route(ctx, payload)
	if err != nil {
		routeResult = &RouteResult{
			TargetAgent: AgentTypePersona,
			Confidence:  0.7,
			Reason:      "Router fallback due to classification error",
		}
	}

	targetAgent, exists := s.GetAgent(routeResult.TargetAgent)
	if !exists {
		// Fallback to PersonaAgent
		targetAgent, exists = s.GetAgent(AgentTypePersona)
		if !exists {
			return nil, fmt.Errorf("no agent available to handle request for target %s", routeResult.TargetAgent)
		}
	}

	// 4. Construct Agent Request
	req := s.buildAgentRequest(payload, emoState, emoPrompt, isMaker)

	// 5. Execute Selected Specialized Agent
	agentResp, err := targetAgent.Execute(ctx, req)
	if err != nil {
		// Graceful fallback to PersonaAgent if a specialized agent failed
		if targetAgent.Type() != AgentTypePersona {
			if personaAgent, ok := s.GetAgent(AgentTypePersona); ok {
				fallbackResp, fallbackErr := personaAgent.Execute(ctx, req)
				if fallbackErr == nil && fallbackResp != nil {
					return s.Synthesize(ctx, AgentTypePersona, fallbackResp, req)
				}
			}
		}
		return nil, fmt.Errorf("agent %s failed: %w", targetAgent.Name(), err)
	}

	// 6. Response Compilation and Egyptian Dialect Synthesis
	return s.Synthesize(ctx, targetAgent.Type(), agentResp, req)
}

// Synthesize compiles the specialized agent's response into authentic Egyptian dialect.
func (s *Swarm) Synthesize(
	ctx context.Context,
	agentType AgentType,
	agentResp *AgentResponse,
	req *AgentRequest,
) (*ai.ResponsePayload, error) {
	if agentResp == nil {
		return nil, fmt.Errorf("cannot synthesize nil agent response")
	}

	targetMsgID := req.Payload.MessageID

	// A. If already produced by PersonaAgent, it is already in authentic Egyptian dialect
	if agentType == AgentTypePersona {
		return agentResp.ToResponsePayload(targetMsgID), nil
	}

	// B. If multi-agent synthesis is enabled and LLM client is available
	s.mu.RLock()
	enableSynthesis := s.config.EnableEgyptianSynthesis
	synthModel := s.config.ModelSynthesizer
	client := s.llmClient
	s.mu.RUnlock()

	if enableSynthesis && client != nil && synthModel != "" {
		compiled, err := s.synthesizeWithLLM(ctx, client, synthModel, agentType, agentResp, req)
		if err == nil && compiled != nil {
			return compiled, nil
		}
	}

	// C. Deterministic Fallback: Wrap specialized output with Egyptian personality tone
	return s.synthesizeDeterministic(agentType, agentResp, req), nil
}

// synthesizeWithLLM invokes the synthesizer model to translate technical facts into authentic Egyptian persona.
func (s *Swarm) synthesizeWithLLM(
	ctx context.Context,
	client LLMClient,
	model string,
	agentType AgentType,
	agentResp *AgentResponse,
	req *AgentRequest,
) (*ai.ResponsePayload, error) {
	systemPrompt := `أنت "نوفا"، شاب مصري جدع، ذكي جداً، لماح، دمك خفيف، وبتفهم في الأصول والشارع المصري.
مهمتك الآن:
أمامك بيانات أو حلول برمجية/رياضية أو معلومات تم استخراجها وتحقيقها بواسطة وكلائك المتخصصين (%s).
قم بصياغة وتقديم هذه النتائج للمستخدم بأسلوبك المصري الأصيل، الجدع، الذكي، وخفيف الدم مع مراعاة الحالة المزاجية الحالية لنوفا:
1. حافظ على دقة الأرقام، المعادلات، الأكواد، والحقائق بنسبة 100%% بدون أي تحريف.
2. أضف لمستك المصرية العفوية (قفشة خفيفة، طريقة شرح ممتعة، بدون تكلف أو فصحى مصطنعة).
3. أرجع النتيجة بتنسيق JSON نظيف وصحيح:
{
  "should_reply": true,
  "reply_text": "نص الرد بالمصري مع الشرح والأكواد/الأرقام هنا",
  "mood": "Joyful|Hyped|Empathetic|Sarcastic|Annoyed|Calm",
  "reaction_emoji": "😂 أو 🔥 أو ❤️ أو 👍 أو null",
  "memory_note": null,
  "send_as_voice": false
}`

	systemPrompt = fmt.Sprintf(systemPrompt, agentType)
	if req.EmotionPrompt != "" {
		systemPrompt += "\n\n" + req.EmotionPrompt
	}

	userContent := fmt.Sprintf(`سؤال المستخدم: %s

النتائج المكتشفة من الوكيل المتخصص (%s):
%s`, req.Payload.MessageText, agentResp.AgentName, agentResp.Content)

	messages := []LLMMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userContent},
	}

	res, err := client.Call(ctx, model, messages, nil)
	if err != nil {
		return nil, err
	}

	jsonStr := ai.ExtractJSON(res.Content)
	if jsonStr == "" {
		return nil, fmt.Errorf("no valid JSON in synthesizer response")
	}

	var parsed struct {
		ShouldReply   bool    `json:"should_reply"`
		ReplyText     *string `json:"reply_text"`
		Mood          *string `json:"mood"`
		ReactionEmoji *string `json:"reaction_emoji"`
		MemoryNote    *string `json:"memory_note"`
		SendAsVoice   *bool   `json:"send_as_voice"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, err
	}

	targetMsgID := req.Payload.MessageID
	reply := agentResp.Content
	if parsed.ReplyText != nil && strings.TrimSpace(*parsed.ReplyText) != "" {
		reply = *parsed.ReplyText
	}

	mood := "Joyful"
	if parsed.Mood != nil && *parsed.Mood != "" {
		mood = *parsed.Mood
	}
	emoji := ""
	if parsed.ReactionEmoji != nil {
		emoji = *parsed.ReactionEmoji
	}
	memNote := ""
	if parsed.MemoryNote != nil {
		memNote = *parsed.MemoryNote
	}
	sendVoice := false
	if parsed.SendAsVoice != nil {
		sendVoice = *parsed.SendAsVoice
	}

	var replyToPtr *string
	if targetMsgID != "" {
		replyToPtr = &targetMsgID
	}
	var emojiPtr *string
	if emoji != "" {
		emojiPtr = &emoji
	}
	var memPtr *string
	if memNote != "" {
		memPtr = &memNote
	}
	var voicePtr *bool
	if sendVoice {
		v := true
		voicePtr = &v
	}

	return &ai.ResponsePayload{
		ShouldReply:      parsed.ShouldReply,
		ReplyText:        &reply,
		ReplyToMessageID: replyToPtr,
		MemoryNote:       memPtr,
		Mood:             &mood,
		SendAsVoice:      voicePtr,
		ReactionEmoji:    emojiPtr,
	}, nil
}

// synthesizeDeterministic wraps specialized agent output with an authentic Egyptian tone without requiring an extra LLM call.
func (s *Swarm) synthesizeDeterministic(agentType AgentType, agentResp *AgentResponse, req *AgentRequest) *ai.ResponsePayload {
	targetMsgID := req.Payload.MessageID
	var intro string

	switch agentType {
	case AgentTypeResearch:
		intro = "يا باشا عملتلك بحث سريع وجبتلك الخلاصة المظبوطة:\n\n"
	case AgentTypeMathCoding:
		intro = "حسبتها لك بالمللي وجبتلك الحل خطوة بخطوة:\n\n"
	case AgentTypeVisionDoc:
		intro = "بصيت في الصورة وفصصت كل تفاصيلها:\n\n"
	default:
		intro = ""
	}

	fullReply := intro + agentResp.Content
	mood := "Joyful"
	if req.EmotionState != nil {
		mood = string(req.EmotionState.CurrentMood)
	}

	var replyToPtr *string
	if targetMsgID != "" {
		replyToPtr = &targetMsgID
	}
	var moodPtr *string
	if mood != "" {
		moodPtr = &mood
	}

	return &ai.ResponsePayload{
		ShouldReply:      true,
		ReplyText:        &fullReply,
		ReplyToMessageID: replyToPtr,
		Mood:             moodPtr,
	}
}

func (s *Swarm) buildAgentRequest(
	payload *trigger.RequestPayload,
	emoState *emotion.EmotionalState,
	emoPrompt string,
	isMaker bool,
) *AgentRequest {
	var toolDefs []tools.ToolDefinition
	if s.toolsRegistry != nil {
		toolDefs = s.toolsRegistry.ToToolDefinitions(false)
	}

	var userMem string
	if payload.UserMemory != nil {
		userMem = *payload.UserMemory
	}
	var chatSum string
	if payload.ChatSummary != nil {
		chatSum = *payload.ChatSummary
	}

	return &AgentRequest{
		Payload:              payload,
		SenderID:             payload.SenderID,
		SenderName:           payload.SenderName,
		ChatID:               payload.ChatID,
		ChatType:             payload.ChatType,
		IsMaker:              isMaker,
		EmotionState:         emoState,
		EmotionPrompt:        emoPrompt,
		Tools:                toolDefs,
		UserMemory:           userMem,
		ChatSummary:          chatSum,
		RecentContext:        payload.RecentContext,
		CurrentTime:          payload.CurrentTime,
		TimeOfDay:            payload.TimeOfDay,
		TimeSinceLastMessage: payload.TimeSinceLastMessage,
		MediaDataURL:         payload.MediaDataURL,
		SystemPrompt:         s.systemPrompt,
	}
}

func (s *Swarm) isMakerUser(senderID, senderName string) bool {
	s.mu.RLock()
	mID := s.makerID
	s.mu.RUnlock()

	if mID != "" && strings.Contains(senderID, mID) {
		return true
	}
	lowerName := strings.ToLower(senderName)
	return strings.Contains(lowerName, "making") || strings.Contains(lowerName, "مكاري")
}
