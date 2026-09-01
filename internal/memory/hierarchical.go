package memory

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryMessage represents a single message in the memory pipeline.
type MemoryMessage struct {
	MessageID  string    `json:"message_id"`
	SenderID   string    `json:"sender_id"`
	SenderName string    `json:"sender_name"`
	Text       string    `json:"text"`
	IsNova     bool      `json:"is_nova"`
	Timestamp  time.Time `json:"timestamp"`
}

// EpisodeSummary represents a compressed episodic memory chunk of conversation.
type EpisodeSummary struct {
	ID           string    `json:"id"`
	ChatID       string    `json:"chat_id"`
	Level        int       `json:"level"` // 1 = Chunk Episode, 2 = Consolidated Episode
	StartTime    time.Time `json:"start_time"`
	EndTime      time.Time `json:"end_time"`
	MessageCount int       `json:"message_count"`
	Summary      string    `json:"summary"`
	KeyTopics    []string  `json:"key_topics,omitempty"`
	Participants []string  `json:"participants,omitempty"`
	KeyDecisions []string  `json:"key_decisions,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	Merged       bool      `json:"merged,omitempty"`
}

// ChatKnowledgeBase represents persistent long-term knowledge synthesized across episodes.
type ChatKnowledgeBase struct {
	ChatID         string            `json:"chat_id"`
	LastUpdated    time.Time         `json:"last_updated"`
	MasterSummary  string            `json:"master_summary"`
	KeyFacts       []string          `json:"key_facts,omitempty"`
	UserInsights   map[string]string `json:"user_insights,omitempty"` // UserID/Name -> Fact/Personality
	TotalEpisodes  int               `json:"total_episodes"`
	TotalMessages  int               `json:"total_messages"`
}

// EpisodeSummarizer defines the interface for compressing messages and synthesizing long-term knowledge.
type EpisodeSummarizer interface {
	SummarizeChunk(ctx context.Context, chatID string, messages []MemoryMessage) (*EpisodeSummary, error)
	SynthesizeLongTermKnowledge(ctx context.Context, chatID string, currentKnowledge *ChatKnowledgeBase, newEpisodes []EpisodeSummary) (*ChatKnowledgeBase, error)
}

// DefaultEpisodeSummarizer provides an offline, deterministic extractive summarizer.
type DefaultEpisodeSummarizer struct{}

// NewDefaultEpisodeSummarizer creates a fallback deterministic summarizer.
func NewDefaultEpisodeSummarizer() *DefaultEpisodeSummarizer {
	return &DefaultEpisodeSummarizer{}
}

func generateEpisodeID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("ep_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
}

// SummarizeChunk extracts participants, key topics, time span, and structured summary from message slice.
func (s *DefaultEpisodeSummarizer) SummarizeChunk(ctx context.Context, chatID string, messages []MemoryMessage) (*EpisodeSummary, error) {
	if len(messages) == 0 {
		return nil, errors.New("cannot summarize empty message slice")
	}

	startTime := messages[0].Timestamp
	endTime := messages[len(messages)-1].Timestamp
	if startTime.IsZero() {
		startTime = time.Now()
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	participantsMap := make(map[string]struct{})
	var lines []string
	topicKeywords := make(map[string]int)

	for _, m := range messages {
		name := m.SenderName
		if m.IsNova {
			name = "Nova"
		}
		if strings.TrimSpace(name) != "" {
			participantsMap[name] = struct{}{}
		}

		trimmedText := strings.TrimSpace(m.Text)
		if trimmedText != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", name, trimmedText))

			// Simple keyword extraction for topics
			words := strings.Fields(trimmedText)
			for _, w := range words {
				wClean := strings.Trim(w, ".,!?:;\"'()[]{}«»")
				if len([]rune(wClean)) >= 4 {
					topicKeywords[wClean]++
				}
			}
		}
	}

	var participants []string
	for p := range participantsMap {
		participants = append(participants, p)
	}

	// Pick top topics
	type topicFreq struct {
		topic string
		count int
	}
	var freqs []topicFreq
	for t, c := range topicKeywords {
		freqs = append(freqs, topicFreq{topic: t, count: c})
	}
	// Sort by count descending
	for i := 0; i < len(freqs); i++ {
		for j := i + 1; j < len(freqs); j++ {
			if freqs[j].count > freqs[i].count {
				freqs[i], freqs[j] = freqs[j], freqs[i]
			}
		}
	}

	var keyTopics []string
	for i := 0; i < len(freqs) && i < 5; i++ {
		keyTopics = append(keyTopics, freqs[i].topic)
	}

	// Build summary text
	var summaryBuilder strings.Builder
	summaryBuilder.WriteString(fmt.Sprintf("محادثة بين (%s) تتناول %d رسائل. ",
		strings.Join(participants, "، "), len(messages)))
	if len(lines) > 0 {
		maxLines := 4
		if len(lines) < maxLines {
			maxLines = len(lines)
		}
		summaryBuilder.WriteString("أبرز النقاط: ")
		summaryBuilder.WriteString(strings.Join(lines[:maxLines], " | "))
	}

	return &EpisodeSummary{
		ID:           generateEpisodeID(),
		ChatID:       chatID,
		Level:        1,
		StartTime:    startTime,
		EndTime:      endTime,
		MessageCount: len(messages),
		Summary:      summaryBuilder.String(),
		KeyTopics:    keyTopics,
		Participants: participants,
		CreatedAt:    time.Now(),
		Merged:       false,
	}, nil
}

// SynthesizeLongTermKnowledge merges new episode summaries into long-term knowledge base.
func (s *DefaultEpisodeSummarizer) SynthesizeLongTermKnowledge(ctx context.Context, chatID string, currentKnowledge *ChatKnowledgeBase, newEpisodes []EpisodeSummary) (*ChatKnowledgeBase, error) {
	if currentKnowledge == nil {
		currentKnowledge = &ChatKnowledgeBase{
			ChatID:       chatID,
			UserInsights: make(map[string]string),
		}
	}
	if currentKnowledge.UserInsights == nil {
		currentKnowledge.UserInsights = make(map[string]string)
	}

	var newSummaries []string
	allFacts := currentKnowledge.KeyFacts
	totalNewMsgs := 0

	for _, ep := range newEpisodes {
		newSummaries = append(newSummaries, fmt.Sprintf("- [%s] %s", ep.StartTime.Format("2006-01-02 15:04"), ep.Summary))
		totalNewMsgs += ep.MessageCount

		for _, p := range ep.Participants {
			if p != "Nova" && p != "" {
				currentKnowledge.UserInsights[p] = fmt.Sprintf("شارك في مناقشات حول: %s", strings.Join(ep.KeyTopics, "، "))
			}
		}

		if len(ep.KeyTopics) > 0 {
			allFacts = append(allFacts, fmt.Sprintf("مواضيع متداولة: %s", strings.Join(ep.KeyTopics, "، ")))
		}
	}

	var master strings.Builder
	if currentKnowledge.MasterSummary != "" {
		master.WriteString(currentKnowledge.MasterSummary)
		master.WriteString("\n\n")
	}
	master.WriteString("### تحديثات المحادثات الأخيرة:\n")
	master.WriteString(strings.Join(newSummaries, "\n"))

	currentKnowledge.MasterSummary = strings.TrimSpace(master.String())
	currentKnowledge.KeyFacts = allFacts
	currentKnowledge.LastUpdated = time.Now()
	currentKnowledge.TotalEpisodes += len(newEpisodes)
	currentKnowledge.TotalMessages += totalNewMsgs

	return currentKnowledge, nil
}

// OpenRouterEpisodeSummarizer uses OpenRouter LLM for intelligent semantic summarization.
type OpenRouterEpisodeSummarizer struct {
	apiKey     string
	model      string
	apiURL     string
	httpClient *http.Client
	fallback   *DefaultEpisodeSummarizer
}

// NewOpenRouterEpisodeSummarizer creates an LLM-backed episode summarizer.
func NewOpenRouterEpisodeSummarizer(apiKey, model string) *OpenRouterEpisodeSummarizer {
	if model == "" {
		model = "google/gemini-3.7-flash"
	}
	return &OpenRouterEpisodeSummarizer{
		apiKey:     apiKey,
		model:      model,
		apiURL:     "https://openrouter.ai/api/v1/chat/completions",
		httpClient: &http.Client{Timeout: 60 * time.Second},
		fallback:   NewDefaultEpisodeSummarizer(),
	}
}

// SetBaseURL overrides the completions endpoint.
func (s *OpenRouterEpisodeSummarizer) SetBaseURL(url string) {
	s.apiURL = url
}

type openRouterSummaryRequest struct {
	Model    string               `json:"model"`
	Messages []openRouterMsgParam `json:"messages"`
}

type openRouterMsgParam struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterSummaryResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func extractJSONString(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start != -1 && end != -1 && end > start {
		return s[start : end+1]
	}
	return s
}

// SummarizeChunk calls the LLM to generate an episodic summary of a conversation chunk.
func (s *OpenRouterEpisodeSummarizer) SummarizeChunk(ctx context.Context, chatID string, messages []MemoryMessage) (*EpisodeSummary, error) {
	if len(messages) == 0 {
		return nil, errors.New("cannot summarize empty messages")
	}

	if s.apiKey == "" {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}

	var b strings.Builder
	for _, m := range messages {
		name := m.SenderName
		if m.IsNova {
			name = "Nova"
		}
		b.WriteString(fmt.Sprintf("[%s] %s: %s\n", m.Timestamp.Format("15:04:05"), name, m.Text))
	}

	prompt := `أنت محرك تلخيص الحلقات والمحادثات لبوت نوفا. حلل مقطع المحادثة التالي واستخرج ملخصاً للحلقة بصيغة JSON نظيفة تماماً بدون أي علامات كود:
{
  "summary": "ملخص واضح وموجز لأحداث هذا المقطع",
  "key_topics": ["موضوع 1", "موضوع 2"],
  "participants": ["اسم 1", "اسم 2"],
  "key_decisions": ["قرار أو نتيجة إن وجدت"]
}`

	reqBody := openRouterSummaryRequest{
		Model: s.model,
		Messages: []openRouterMsgParam{
			{Role: "system", Content: prompt},
			{Role: "user", Content: b.String()},
		},
	}

	reqData, err := json.Marshal(reqBody)
	if err != nil {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL, bytes.NewReader(reqData))
	if err != nil {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova Hierarchical Memory")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}

	var orResp openRouterSummaryResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil || len(orResp.Choices) == 0 {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}

	jsonStr := extractJSONString(orResp.Choices[0].Message.Content)
	var parsed struct {
		Summary      string   `json:"summary"`
		KeyTopics    []string `json:"key_topics"`
		Participants []string `json:"participants"`
		KeyDecisions []string `json:"key_decisions"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil || strings.TrimSpace(parsed.Summary) == "" {
		return s.fallback.SummarizeChunk(ctx, chatID, messages)
	}

	startTime := messages[0].Timestamp
	endTime := messages[len(messages)-1].Timestamp
	if startTime.IsZero() {
		startTime = time.Now()
	}
	if endTime.IsZero() {
		endTime = time.Now()
	}

	return &EpisodeSummary{
		ID:           generateEpisodeID(),
		ChatID:       chatID,
		Level:        1,
		StartTime:    startTime,
		EndTime:      endTime,
		MessageCount: len(messages),
		Summary:      parsed.Summary,
		KeyTopics:    parsed.KeyTopics,
		Participants: parsed.Participants,
		KeyDecisions: parsed.KeyDecisions,
		CreatedAt:    time.Now(),
		Merged:       false,
	}, nil
}

// SynthesizeLongTermKnowledge merges episodes into master knowledge base with LLM.
func (s *OpenRouterEpisodeSummarizer) SynthesizeLongTermKnowledge(ctx context.Context, chatID string, currentKnowledge *ChatKnowledgeBase, newEpisodes []EpisodeSummary) (*ChatKnowledgeBase, error) {
	if s.apiKey == "" {
		return s.fallback.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, newEpisodes)
	}

	if currentKnowledge == nil {
		currentKnowledge = &ChatKnowledgeBase{
			ChatID:       chatID,
			UserInsights: make(map[string]string),
		}
	}
	if currentKnowledge.UserInsights == nil {
		currentKnowledge.UserInsights = make(map[string]string)
	}

	var episodesText strings.Builder
	for _, ep := range newEpisodes {
		episodesText.WriteString(fmt.Sprintf("- حلقة (%s إلى %s) [%d رسائل]: %s (المواضيع: %s | المشاركون: %s)\n",
			ep.StartTime.Format("2006-01-02 15:04"), ep.EndTime.Format("15:04"),
			ep.MessageCount, ep.Summary, strings.Join(ep.KeyTopics, ", "), strings.Join(ep.Participants, ", ")))
	}

	systemPrompt := `أنت محرك إدارة المعرفة التراكمية طويلة المدى (Long-Term Knowledge Base) لبوت نوفا.
مهمتك دمج ملخصات الحلقات الجديدة مع المعرفة التراكمية السابقة لإنتاج قاعدة معرفية متماسكة ودائمة.
أرجع JSON فقط:
{
  "master_summary": "ملخص شامل متكامل يربط تاريخ الشات بالنقاط الجديدة",
  "key_facts": ["حقيقة أو قرار مهم 1", "حقيقة أو قرار 2"],
  "user_insights": {
    "اسم_المستخدم": "معلومات ورؤى عن اهتماماته وسلوكه"
  }
}`

	userPrompt := fmt.Sprintf("المعرفة السابقة:\n%s\n\nالحلقات الجديدة لدمجها:\n%s",
		currentKnowledge.MasterSummary, episodesText.String())

	reqBody := openRouterSummaryRequest{
		Model: s.model,
		Messages: []openRouterMsgParam{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	reqData, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.apiURL, bytes.NewReader(reqData))
	if err != nil {
		return s.fallback.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, newEpisodes)
	}

	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova Long-Term Knowledge Synthesis")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return s.fallback.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, newEpisodes)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return s.fallback.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, newEpisodes)
	}

	respBytes, _ := io.ReadAll(resp.Body)
	var orResp openRouterSummaryResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil || len(orResp.Choices) == 0 {
		return s.fallback.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, newEpisodes)
	}

	jsonStr := extractJSONString(orResp.Choices[0].Message.Content)
	var parsed struct {
		MasterSummary string            `json:"master_summary"`
		KeyFacts      []string          `json:"key_facts"`
		UserInsights  map[string]string `json:"user_insights"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil || strings.TrimSpace(parsed.MasterSummary) == "" {
		return s.fallback.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, newEpisodes)
	}

	totalMsgs := 0
	for _, ep := range newEpisodes {
		totalMsgs += ep.MessageCount
	}

	currentKnowledge.MasterSummary = parsed.MasterSummary
	currentKnowledge.KeyFacts = parsed.KeyFacts
	for k, v := range parsed.UserInsights {
		currentKnowledge.UserInsights[k] = v
	}
	currentKnowledge.LastUpdated = time.Now()
	currentKnowledge.TotalEpisodes += len(newEpisodes)
	currentKnowledge.TotalMessages += totalMsgs

	return currentKnowledge, nil
}

// HierarchicalMemory manages message chunking, episode summaries, and long-term knowledge base per chat.
type HierarchicalMemory struct {
	baseDir    string
	chunkSize  int
	summarizer EpisodeSummarizer
	mu         sync.RWMutex
	chatMu     map[string]*sync.RWMutex
	buffers    map[string][]MemoryMessage
}

// NewHierarchicalMemory creates a new HierarchicalMemory manager.
// Default base directory is "data/memory" and default chunkSize is 15 messages.
func NewHierarchicalMemory(baseDir string, chunkSize int, summarizer EpisodeSummarizer) (*HierarchicalMemory, error) {
	if baseDir == "" {
		baseDir = "data/memory"
	}
	if chunkSize <= 0 {
		chunkSize = 15
	}
	if summarizer == nil {
		summarizer = NewDefaultEpisodeSummarizer()
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory directory %s: %w", baseDir, err)
	}

	return &HierarchicalMemory{
		baseDir:    baseDir,
		chunkSize:  chunkSize,
		summarizer: summarizer,
		chatMu:     make(map[string]*sync.RWMutex),
		buffers:    make(map[string][]MemoryMessage),
	}, nil
}

func (h *HierarchicalMemory) getChatMutex(chatID string) *sync.RWMutex {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m, ok := h.chatMu[chatID]; ok {
		return m
	}
	m := &sync.RWMutex{}
	h.chatMu[chatID] = m
	return m
}

func (h *HierarchicalMemory) getEpisodesPath(chatID string) string {
	safeID := sanitizeChatID(chatID)
	return filepath.Join(h.baseDir, fmt.Sprintf("episodes_%s.json", safeID))
}

func (h *HierarchicalMemory) getKnowledgeJSONPath(chatID string) string {
	safeID := sanitizeChatID(chatID)
	return filepath.Join(h.baseDir, fmt.Sprintf("knowledge_%s.json", safeID))
}

func (h *HierarchicalMemory) getKnowledgeMDPath(chatID string) string {
	safeID := sanitizeChatID(chatID)
	return filepath.Join(h.baseDir, fmt.Sprintf("knowledge_%s.md", safeID))
}

// AddMessage appends a message to the chat's buffer and returns true if the buffer has reached the chunk threshold.
func (h *HierarchicalMemory) AddMessage(chatID string, msg MemoryMessage) bool {
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	mu := h.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	h.buffers[chatID] = append(h.buffers[chatID], msg)
	return len(h.buffers[chatID]) >= h.chunkSize
}

// GetBuffer returns a copy of the current buffered messages for a chat.
func (h *HierarchicalMemory) GetBuffer(chatID string) []MemoryMessage {
	mu := h.getChatMutex(chatID)
	mu.RLock()
	defer mu.RUnlock()

	buf := h.buffers[chatID]
	copied := make([]MemoryMessage, len(buf))
	copy(copied, buf)
	return copied
}

// CompressChunk compresses buffered messages for a chat into an EpisodeSummary and saves it.
func (h *HierarchicalMemory) CompressChunk(ctx context.Context, chatID string) (*EpisodeSummary, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}

	mu := h.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	buf := h.buffers[chatID]
	if len(buf) == 0 {
		return nil, errors.New("no messages buffered to compress")
	}

	// Pull messages up to chunkSize or entire buffer
	count := h.chunkSize
	if len(buf) < count {
		count = len(buf)
	}

	chunkToCompress := make([]MemoryMessage, count)
	copy(chunkToCompress, buf[:count])
	h.buffers[chatID] = buf[count:]

	// Summarize
	episode, err := h.summarizer.SummarizeChunk(ctx, chatID, chunkToCompress)
	if err != nil {
		return nil, fmt.Errorf("failed to summarize chunk: %w", err)
	}

	// Save episode
	episodes, _ := h.loadEpisodesLocked(chatID)
	episodes = append(episodes, *episode)
	if err := h.saveEpisodesLocked(chatID, episodes); err != nil {
		return nil, err
	}

	return episode, nil
}

// CompressMessages directly compresses an explicit slice of messages into an EpisodeSummary and saves it.
func (h *HierarchicalMemory) CompressMessages(ctx context.Context, chatID string, messages []MemoryMessage) (*EpisodeSummary, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}
	if len(messages) == 0 {
		return nil, errors.New("messages slice cannot be empty")
	}

	mu := h.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	episode, err := h.summarizer.SummarizeChunk(ctx, chatID, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to summarize messages: %w", err)
	}

	episodes, _ := h.loadEpisodesLocked(chatID)
	episodes = append(episodes, *episode)
	if err := h.saveEpisodesLocked(chatID, episodes); err != nil {
		return nil, err
	}

	return episode, nil
}

func (h *HierarchicalMemory) loadEpisodesLocked(chatID string) ([]EpisodeSummary, error) {
	filePath := h.getEpisodesPath(chatID)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var episodes []EpisodeSummary
	if err := json.Unmarshal(data, &episodes); err != nil {
		return nil, err
	}

	return episodes, nil
}

func (h *HierarchicalMemory) saveEpisodesLocked(chatID string, episodes []EpisodeSummary) error {
	filePath := h.getEpisodesPath(chatID)
	data, err := json.MarshalIndent(episodes, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(filePath)
		if renameErr := os.Rename(tmpPath, filePath); renameErr != nil {
			_ = os.WriteFile(filePath, data, 0644)
			_ = os.Remove(tmpPath)
		}
	}

	return nil
}

// GetEpisodes returns all stored episode summaries for a chat.
func (h *HierarchicalMemory) GetEpisodes(chatID string) ([]EpisodeSummary, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}

	mu := h.getChatMutex(chatID)
	mu.RLock()
	defer mu.RUnlock()

	episodes, err := h.loadEpisodesLocked(chatID)
	if err != nil {
		if os.IsNotExist(err) {
			return []EpisodeSummary{}, nil
		}
		return nil, err
	}

	return episodes, nil
}

// MergeEpisodesToLongTerm consolidates unmerged episode summaries into the persistent knowledge base.
func (h *HierarchicalMemory) MergeEpisodesToLongTerm(ctx context.Context, chatID string) (*ChatKnowledgeBase, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}

	mu := h.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	episodes, err := h.loadEpisodesLocked(chatID)
	if err != nil {
		if os.IsNotExist(err) {
			return h.loadKnowledgeLocked(chatID)
		}
		return nil, err
	}

	var unmerged []EpisodeSummary
	for _, ep := range episodes {
		if !ep.Merged {
			unmerged = append(unmerged, ep)
		}
	}

	currentKnowledge, _ := h.loadKnowledgeLocked(chatID)
	if currentKnowledge == nil {
		currentKnowledge = &ChatKnowledgeBase{
			ChatID:       chatID,
			UserInsights: make(map[string]string),
		}
	}

	if len(unmerged) == 0 {
		return currentKnowledge, nil
	}

	// Synthesize
	updatedKnowledge, err := h.summarizer.SynthesizeLongTermKnowledge(ctx, chatID, currentKnowledge, unmerged)
	if err != nil {
		return nil, fmt.Errorf("failed to synthesize long-term knowledge: %w", err)
	}

	// Mark episodes as merged
	for i := range episodes {
		episodes[i].Merged = true
	}
	if err := h.saveEpisodesLocked(chatID, episodes); err != nil {
		return nil, err
	}

	// Save knowledge base
	if err := h.saveKnowledgeLocked(chatID, updatedKnowledge); err != nil {
		return nil, err
	}

	return updatedKnowledge, nil
}

func (h *HierarchicalMemory) loadKnowledgeLocked(chatID string) (*ChatKnowledgeBase, error) {
	filePath := h.getKnowledgeJSONPath(chatID)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ChatKnowledgeBase{
				ChatID:       chatID,
				LastUpdated:  time.Now(),
				UserInsights: make(map[string]string),
			}, nil
		}
		return nil, err
	}

	var kb ChatKnowledgeBase
	if err := json.Unmarshal(data, &kb); err != nil {
		return nil, err
	}

	if kb.UserInsights == nil {
		kb.UserInsights = make(map[string]string)
	}

	return &kb, nil
}

func (h *HierarchicalMemory) saveKnowledgeLocked(chatID string, kb *ChatKnowledgeBase) error {
	jsonPath := h.getKnowledgeJSONPath(chatID)
	data, err := json.MarshalIndent(kb, "", "  ")
	if err != nil {
		return err
	}

	tmpJSON := jsonPath + ".tmp"
	if err := os.WriteFile(tmpJSON, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpJSON, jsonPath); err != nil {
		_ = os.Remove(jsonPath)
		_ = os.Rename(tmpJSON, jsonPath)
	}

	// Save human-readable markdown file as well
	mdPath := h.getKnowledgeMDPath(chatID)
	var md strings.Builder
	md.WriteString(fmt.Sprintf("# 🧠 قاعدة المعرفة التراكمية للشات: %s\n", chatID))
	md.WriteString(fmt.Sprintf("**آخر تحديث:** %s\n", kb.LastUpdated.Format("2006-01-02 15:04:05")))
	md.WriteString(fmt.Sprintf("**إجمالي الحلقات:** %d | **إجمالي الرسائل:** %d\n\n---\n\n", kb.TotalEpisodes, kb.TotalMessages))

	md.WriteString("## 📜 الملخص الشامل لتاريخ الشات:\n")
	md.WriteString(kb.MasterSummary)
	md.WriteString("\n\n")

	if len(kb.KeyFacts) > 0 {
		md.WriteString("## 📌 أهم الحقائق والقرارات المتفق عليها:\n")
		for _, f := range kb.KeyFacts {
			md.WriteString(fmt.Sprintf("- %s\n", f))
		}
		md.WriteString("\n")
	}

	if len(kb.UserInsights) > 0 {
		md.WriteString("## 👤 رؤى المستخدمين والمشاركين:\n")
		for user, insight := range kb.UserInsights {
			md.WriteString(fmt.Sprintf("- **%s**: %s\n", user, insight))
		}
		md.WriteString("\n")
	}

	_ = os.WriteFile(mdPath, []byte(md.String()), 0644)
	return nil
}

// GetLongTermKnowledge returns the persistent knowledge base for a chat.
func (h *HierarchicalMemory) GetLongTermKnowledge(chatID string) (*ChatKnowledgeBase, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}

	mu := h.getChatMutex(chatID)
	mu.RLock()
	defer mu.RUnlock()

	return h.loadKnowledgeLocked(chatID)
}

// SaveKnowledgeBase directly persists a knowledge base object.
func (h *HierarchicalMemory) SaveKnowledgeBase(chatID string, kb *ChatKnowledgeBase) error {
	if strings.TrimSpace(chatID) == "" {
		return errors.New("chatID cannot be empty")
	}
	if kb == nil {
		return errors.New("knowledge base cannot be nil")
	}

	mu := h.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	return h.saveKnowledgeLocked(chatID, kb)
}

// FormatKnowledgePrompt produces a rich Markdown block for LLM system prompt injection.
func (h *HierarchicalMemory) FormatKnowledgePrompt(chatID string) (string, error) {
	kb, err := h.GetLongTermKnowledge(chatID)
	if err != nil || kb == nil || (kb.MasterSummary == "" && len(kb.KeyFacts) == 0 && len(kb.UserInsights) == 0) {
		return "", nil
	}

	var b strings.Builder
	b.WriteString("### 🧠 الذاكرة التراكمية والمعرفة الدائمة للشات (Chat Knowledge Base):\n")
	if kb.MasterSummary != "" {
		b.WriteString(fmt.Sprintf("**الملخص الشامل:** %s\n", kb.MasterSummary))
	}
	if len(kb.KeyFacts) > 0 {
		b.WriteString("**أهم الحقائق والقرارات:**\n")
		for _, f := range kb.KeyFacts {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}
	if len(kb.UserInsights) > 0 {
		b.WriteString("**رؤى المشاركين:**\n")
		for user, insight := range kb.UserInsights {
			b.WriteString(fmt.Sprintf("- %s: %s\n", user, insight))
		}
	}

	return strings.TrimSpace(b.String()), nil
}

// ClearChatMemory wipes episodes, buffers, and knowledge base for a chat.
func (h *HierarchicalMemory) ClearChatMemory(chatID string) error {
	mu := h.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	delete(h.buffers, chatID)
	_ = os.Remove(h.getEpisodesPath(chatID))
	_ = os.Remove(h.getKnowledgeJSONPath(chatID))
	_ = os.Remove(h.getKnowledgeMDPath(chatID))
	return nil
}
