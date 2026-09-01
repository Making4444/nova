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
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CosineSimilarity calculates the cosine similarity between two float32 vectors.
// Returns a value between -1.0 and 1.0.
// If vectors have different lengths or either is zero-length, an error is returned.
// If either vector has zero magnitude (zero vector), 0.0 is returned without error.
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) == 0 || len(b) == 0 {
		return 0, errors.New("cannot compute cosine similarity for empty vector")
	}
	if len(a) != len(b) {
		return 0, fmt.Errorf("vector dimensions mismatch: %d != %d", len(a), len(b))
	}

	var dotProduct float64
	var normA float64
	var normB float64

	for i := 0; i < len(a); i++ {
		valA := float64(a[i])
		valB := float64(b[i])
		dotProduct += valA * valB
		normA += valA * valA
		normB += valB * valB
	}

	if normA == 0 || normB == 0 {
		return 0, nil
	}

	sim := dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
	// Clamp to [-1.0, 1.0] to guard against floating-point inaccuracies
	if sim > 1.0 {
		sim = 1.0
	} else if sim < -1.0 {
		sim = -1.0
	}

	return float32(sim), nil
}

// DotProduct calculates the scalar dot product of two vectors.
func DotProduct(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("vector dimensions mismatch: %d != %d", len(a), len(b))
	}
	var dot float32
	for i := range a {
		dot += a[i] * b[i]
	}
	return dot, nil
}

// VectorMagnitude calculates the Euclidean L2 norm of a vector.
func VectorMagnitude(a []float32) float32 {
	var sum float64
	for _, v := range a {
		sum += float64(v) * float64(v)
	}
	return float32(math.Sqrt(sum))
}

// NormalizeVector returns the L2-normalized unit vector.
func NormalizeVector(a []float32) []float32 {
	mag := VectorMagnitude(a)
	if mag == 0 {
		out := make([]float32, len(a))
		return out
	}
	out := make([]float32, len(a))
	for i, v := range a {
		out[i] = v / mag
	}
	return out
}

// MemoryItem represents a semantic memory item stored in vector storage.
type MemoryItem struct {
	ID        string            `json:"id"`
	ChatID    string            `json:"chat_id"`
	UserID    string            `json:"user_id"`
	UserName  string            `json:"user_name"`
	FactText  string            `json:"fact_text"`
	Embedding []float32         `json:"embedding,omitempty"`
	Timestamp string            `json:"timestamp"`
	Score     float32           `json:"score,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// VectorDocument represents the persistent file container for a chat's vector memories.
type VectorDocument struct {
	ChatID      string       `json:"chat_id"`
	LastUpdated string       `json:"last_updated"`
	Count       int          `json:"count"`
	Memories    []MemoryItem `json:"memories"`
}

// Embedder generates vector embeddings for text.
type Embedder interface {
	GetEmbedding(ctx context.Context, text string) ([]float32, error)
	GetBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
}

// OpenRouterEmbedder implements Embedder using OpenRouter or standard OpenAI-compatible embedding APIs.
type OpenRouterEmbedder struct {
	apiKey     string
	model      string
	apiURL     string
	httpClient *http.Client
}

// NewOpenRouterEmbedder creates an embedder client for OpenRouter or OpenAI compatible endpoints.
func NewOpenRouterEmbedder(apiKey, model string) *OpenRouterEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenRouterEmbedder{
		apiKey: apiKey,
		model:  model,
		apiURL: "https://openrouter.ai/api/v1/embeddings",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetBaseURL allows overriding the API endpoint (useful for custom proxies and testing).
func (e *OpenRouterEmbedder) SetBaseURL(url string) {
	e.apiURL = url
}

type embeddingRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"`
}

type embeddingItem struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

type embeddingResponse struct {
	Data  []embeddingItem `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// GetEmbedding returns the embedding vector for a single text string.
func (e *OpenRouterEmbedder) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	batch, err := e.GetBatchEmbeddings(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(batch) == 0 {
		return nil, errors.New("empty embedding response received from API")
	}
	return batch[0], nil
}

// GetBatchEmbeddings returns embedding vectors for a batch of text strings.
func (e *OpenRouterEmbedder) GetBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	if e.apiKey == "" {
		return nil, errors.New("embedding API key is not configured")
	}

	reqBody := embeddingRequest{
		Model: e.model,
		Input: texts,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.apiURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/makari/novabot")
	req.Header.Set("X-Title", "Nova Bot Vector Memory")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedding response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	var apiResp embeddingResponse
	if err := json.Unmarshal(respBytes, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	if apiResp.Error != nil {
		return nil, fmt.Errorf("embedding API returned error: %s (code: %d)", apiResp.Error.Message, apiResp.Error.Code)
	}

	if len(apiResp.Data) == 0 {
		return nil, errors.New("embedding API returned empty data list")
	}

	// Sort results by index to ensure proper sequential ordering
	sort.Slice(apiResp.Data, func(i, j int) bool {
		return apiResp.Data[i].Index < apiResp.Data[j].Index
	})

	results := make([][]float32, len(texts))
	for i, item := range apiResp.Data {
		if i < len(results) {
			results[i] = item.Embedding
		}
	}

	return results, nil
}

// VectorStore manages local JSON-persisted vector storage and semantic memory retrieval.
type VectorStore struct {
	baseDir  string
	embedder Embedder
	mu       sync.RWMutex
	chatMu   map[string]*sync.RWMutex
}

// NewVectorStore initializes the VectorStore in the specified directory.
// Default base directory is "data/memory".
func NewVectorStore(baseDir string, embedder Embedder) (*VectorStore, error) {
	if baseDir == "" {
		baseDir = "data/memory"
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create vector memory directory %s: %w", baseDir, err)
	}

	return &VectorStore{
		baseDir:  baseDir,
		embedder: embedder,
		chatMu:   make(map[string]*sync.RWMutex),
	}, nil
}

// SetEmbedder updates the embedder client.
func (s *VectorStore) SetEmbedder(embedder Embedder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embedder = embedder
}

func sanitizeChatID(id string) string {
	replacer := strings.NewReplacer(":", "_", "/", "_", "\\", "_", "<", "_", ">", "_", "|", "_", "?", "_", "*", "_", "\"", "_", " ", "_")
	return replacer.Replace(id)
}

func (s *VectorStore) getFilePath(chatID string) string {
	safeID := sanitizeChatID(chatID)
	return filepath.Join(s.baseDir, fmt.Sprintf("vectors_%s.json", safeID))
}

func (s *VectorStore) getChatMutex(chatID string) *sync.RWMutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.chatMu[chatID]; ok {
		return m
	}
	m := &sync.RWMutex{}
	s.chatMu[chatID] = m
	return m
}

func generateMemoryID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("mem_%d_%s", time.Now().UnixNano(), hex.EncodeToString(b))
}

func (s *VectorStore) loadMemoriesLocked(chatID string) ([]MemoryItem, error) {
	filePath := s.getFilePath(chatID)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[]" {
		return []MemoryItem{}, nil
	}

	// Try reading structured VectorDocument first
	var doc VectorDocument
	if err := json.Unmarshal(data, &doc); err == nil && len(doc.Memories) > 0 {
		return doc.Memories, nil
	}

	// Fallback: try direct slice unmarshaling
	var rawSlice []MemoryItem
	if err := json.Unmarshal(data, &rawSlice); err == nil {
		return rawSlice, nil
	}

	return []MemoryItem{}, nil
}

func (s *VectorStore) saveMemoriesLocked(chatID string, memories []MemoryItem) error {
	filePath := s.getFilePath(chatID)
	doc := VectorDocument{
		ChatID:      chatID,
		LastUpdated: time.Now().Format(time.RFC3339),
		Count:       len(memories),
		Memories:    memories,
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal vector document: %w", err)
	}

	// Atomic write via temp file
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary vector memory file %s: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, filePath); err != nil {
		// Fallback for Windows if destination exists
		_ = os.Remove(filePath)
		if renameErr := os.Rename(tmpPath, filePath); renameErr != nil {
			_ = os.WriteFile(filePath, data, 0644)
			_ = os.Remove(tmpPath)
		}
	}

	return nil
}

// SaveMemory stores a new fact memory item for a chat/user with its embedding.
// If embedding is empty and an Embedder is configured, an embedding is automatically generated.
func (s *VectorStore) SaveMemory(chatID, userID, userName, factText string, embedding []float32) error {
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(factText) == "" {
		return errors.New("chatID and factText cannot be empty")
	}

	s.mu.RLock()
	embClient := s.embedder
	s.mu.RUnlock()

	// Auto-generate embedding if missing and embedder is available
	if len(embedding) == 0 && embClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if emb, err := embClient.GetEmbedding(ctx, factText); err == nil && len(emb) > 0 {
			embedding = emb
		}
	}

	mu := s.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	memories, err := s.loadMemoriesLocked(chatID)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load memories for chat %s: %w", chatID, err)
	}

	item := MemoryItem{
		ID:        generateMemoryID(),
		ChatID:    chatID,
		UserID:    userID,
		UserName:  userName,
		FactText:  strings.TrimSpace(factText),
		Embedding: embedding,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Update existing memory if identical fact text already exists for this user
	updated := false
	for i, m := range memories {
		if m.UserID == userID && strings.EqualFold(strings.TrimSpace(m.FactText), strings.TrimSpace(factText)) {
			memories[i].Timestamp = item.Timestamp
			if len(embedding) > 0 {
				memories[i].Embedding = embedding
			}
			if userName != "" {
				memories[i].UserName = userName
			}
			updated = true
			break
		}
	}

	if !updated {
		memories = append(memories, item)
	}

	return s.saveMemoriesLocked(chatID, memories)
}

// SearchRelevantMemories searches memories in a chat by cosine similarity against queryEmbedding.
// Returns up to topK items with score >= minScore, sorted descending by relevance score.
func (s *VectorStore) SearchRelevantMemories(chatID string, queryEmbedding []float32, topK int, minScore float32) ([]MemoryItem, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}
	if len(queryEmbedding) == 0 {
		return nil, errors.New("queryEmbedding cannot be empty")
	}

	mu := s.getChatMutex(chatID)
	mu.RLock()
	defer mu.RUnlock()

	memories, err := s.loadMemoriesLocked(chatID)
	if err != nil {
		if os.IsNotExist(err) {
			return []MemoryItem{}, nil
		}
		return nil, err
	}

	type scoredItem struct {
		item  MemoryItem
		score float32
	}

	var scored []scoredItem
	for _, m := range memories {
		if len(m.Embedding) == 0 {
			continue
		}
		sim, err := CosineSimilarity(queryEmbedding, m.Embedding)
		if err != nil {
			continue
		}
		if sim >= minScore {
			mCopy := m
			mCopy.Score = sim
			scored = append(scored, scoredItem{
				item:  mCopy,
				score: sim,
			})
		}
	}

	// Sort descending by score
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if topK > 0 && len(scored) > topK {
		scored = scored[:topK]
	}

	results := make([]MemoryItem, len(scored))
	for i, sItem := range scored {
		results[i] = sItem.item
	}

	return results, nil
}

// DeleteMemory removes a specific memory item by memoryID from a chat.
func (s *VectorStore) DeleteMemory(chatID string, memoryID string) error {
	if strings.TrimSpace(chatID) == "" || strings.TrimSpace(memoryID) == "" {
		return errors.New("chatID and memoryID cannot be empty")
	}

	mu := s.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	memories, err := s.loadMemoriesLocked(chatID)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	filtered := make([]MemoryItem, 0, len(memories))
	found := false
	for _, m := range memories {
		if m.ID == memoryID {
			found = true
			continue
		}
		filtered = append(filtered, m)
	}

	if !found {
		return nil // Idempotent
	}

	return s.saveMemoriesLocked(chatID, filtered)
}

// GetUserMemories retrieves all stored memory items for a specific user in a chat.
func (s *VectorStore) GetUserMemories(chatID, userID string) ([]MemoryItem, error) {
	if strings.TrimSpace(chatID) == "" {
		return nil, errors.New("chatID cannot be empty")
	}

	mu := s.getChatMutex(chatID)
	mu.RLock()
	defer mu.RUnlock()

	memories, err := s.loadMemoriesLocked(chatID)
	if err != nil {
		if os.IsNotExist(err) {
			return []MemoryItem{}, nil
		}
		return nil, err
	}

	var results []MemoryItem
	for _, m := range memories {
		if userID == "" || m.UserID == userID {
			results = append(results, m)
		}
	}

	return results, nil
}

// GetAllMemories returns all memory items stored for a chat.
func (s *VectorStore) GetAllMemories(chatID string) ([]MemoryItem, error) {
	return s.GetUserMemories(chatID, "")
}

// SearchByText embeds the query string and performs semantic search.
func (s *VectorStore) SearchByText(ctx context.Context, chatID, queryText string, topK int, minScore float32) ([]MemoryItem, error) {
	s.mu.RLock()
	embClient := s.embedder
	s.mu.RUnlock()

	if embClient == nil {
		return nil, errors.New("no embedder configured for text-based semantic search")
	}

	queryEmbedding, err := embClient.GetEmbedding(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}

	return s.SearchRelevantMemories(chatID, queryEmbedding, topK, minScore)
}

// ClearChatMemories deletes all vector memories for a specific chat.
func (s *VectorStore) ClearChatMemories(chatID string) error {
	mu := s.getChatMutex(chatID)
	mu.Lock()
	defer mu.Unlock()

	filePath := s.getFilePath(chatID)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
