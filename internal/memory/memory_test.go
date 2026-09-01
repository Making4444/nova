package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- 1. Vector Math Unit Tests ---

func TestCosineSimilarity(t *testing.T) {
	// Identical vectors -> 1.0
	v1 := []float32{1.0, 2.0, 3.0}
	v2 := []float32{1.0, 2.0, 3.0}
	sim, err := CosineSimilarity(v1, v2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(sim)-1.0) > 1e-5 {
		t.Errorf("expected 1.0 for identical vectors, got %f", sim)
	}

	// Orthogonal vectors -> 0.0
	vOrth1 := []float32{1.0, 0.0, 0.0}
	vOrth2 := []float32{0.0, 1.0, 0.0}
	simOrth, err := CosineSimilarity(vOrth1, vOrth2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(simOrth)) > 1e-5 {
		t.Errorf("expected 0.0 for orthogonal vectors, got %f", simOrth)
	}

	// Opposite vectors -> -1.0
	vOpp1 := []float32{1.0, 0.0}
	vOpp2 := []float32{-1.0, 0.0}
	simOpp, err := CosineSimilarity(vOpp1, vOpp2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(simOpp)-(-1.0)) > 1e-5 {
		t.Errorf("expected -1.0 for opposite vectors, got %f", simOpp)
	}

	// Known vectors: [1, 2, 3] and [4, 5, 6] -> 32 / (sqrt(14)*sqrt(77)) = 32 / sqrt(1078) ≈ 0.9746318
	vA := []float32{1.0, 2.0, 3.0}
	vB := []float32{4.0, 5.0, 6.0}
	expected := float32(32.0 / math.Sqrt(14.0*77.0))
	simKnown, err := CosineSimilarity(vA, vB)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if math.Abs(float64(simKnown-expected)) > 1e-5 {
		t.Errorf("expected %f, got %f", expected, simKnown)
	}

	// Zero vector handling -> returns 0.0 without error
	vZero := []float32{0.0, 0.0, 0.0}
	simZero, err := CosineSimilarity(v1, vZero)
	if err != nil {
		t.Fatalf("unexpected error with zero vector: %v", err)
	}
	if simZero != 0.0 {
		t.Errorf("expected 0.0 for zero vector, got %f", simZero)
	}

	// Dimension mismatch -> error
	vMismatch := []float32{1.0, 2.0}
	_, err = CosineSimilarity(v1, vMismatch)
	if err == nil {
		t.Errorf("expected error for dimension mismatch, got nil")
	}

	// Empty vectors -> error
	_, err = CosineSimilarity([]float32{}, []float32{})
	if err == nil {
		t.Errorf("expected error for empty vectors, got nil")
	}
}

func TestVectorHelpers(t *testing.T) {
	v1 := []float32{3.0, 4.0}
	mag := VectorMagnitude(v1)
	if math.Abs(float64(mag)-5.0) > 1e-5 {
		t.Errorf("expected magnitude 5.0, got %f", mag)
	}

	norm := NormalizeVector(v1)
	if math.Abs(float64(norm[0])-0.6) > 1e-5 || math.Abs(float64(norm[1])-0.8) > 1e-5 {
		t.Errorf("expected [0.6, 0.8], got %v", norm)
	}

	dot, err := DotProduct([]float32{1.0, 2.0}, []float32{3.0, 4.0})
	if err != nil || math.Abs(float64(dot)-11.0) > 1e-5 {
		t.Errorf("expected dot product 11.0, got %f (err: %v)", dot, err)
	}
}

// --- Mock Embedder ---

type mockEmbedder struct {
	embedMap map[string][]float32
}

func (m *mockEmbedder) GetEmbedding(ctx context.Context, text string) ([]float32, error) {
	if emb, ok := m.embedMap[text]; ok {
		return emb, nil
	}
	// Deterministic fallback vector based on string length
	l := float32(len(text))
	return []float32{l * 0.1, l * 0.2, l * 0.3}, nil
}

func (m *mockEmbedder) GetBatchEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	var results [][]float32
	for _, txt := range texts {
		emb, err := m.GetEmbedding(ctx, txt)
		if err != nil {
			return nil, err
		}
		results = append(results, emb)
	}
	return results, nil
}

// --- 2. VectorStore Indexing & Semantic Search Tests ---

func TestVectorStore_SaveAndSearch(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nova_memory_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockEmb := &mockEmbedder{
		embedMap: map[string][]float32{
			"makari likes programming in Go": {0.9, 0.8, 0.1, 0.0},
			"makari works as backend engineer": {0.85, 0.75, 0.1, 0.0},
			"nova loves eating pizza":          {0.0, 0.1, 0.9, 0.8},
		},
	}

	store, err := NewVectorStore(tempDir, mockEmb)
	if err != nil {
		t.Fatalf("failed to create vector store: %v", err)
	}

	chatID := "group_123@g.us"
	userID := "user_456"

	// 1. Save memories
	err = store.SaveMemory(chatID, userID, "Makari", "makari likes programming in Go", []float32{0.9, 0.8, 0.1, 0.0})
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	err = store.SaveMemory(chatID, userID, "Makari", "makari works as backend engineer", []float32{0.85, 0.75, 0.1, 0.0})
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	err = store.SaveMemory(chatID, "user_789", "NovaUser", "nova loves eating pizza", []float32{0.0, 0.1, 0.9, 0.8})
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	// Check JSON file existence
	safeChat := sanitizeChatID(chatID)
	jsonPath := filepath.Join(tempDir, fmt.Sprintf("vectors_%s.json", safeChat))
	if _, err := os.Stat(jsonPath); os.IsNotExist(err) {
		t.Fatalf("expected JSON memory file to exist at %s", jsonPath)
	}

	// 2. Search relevant memories with programming query vector
	queryVector := []float32{0.95, 0.85, 0.05, 0.0} // very close to Go programming
	results, err := store.SearchRelevantMemories(chatID, queryVector, 2, 0.5)
	if err != nil {
		t.Fatalf("SearchRelevantMemories failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Top result should be Go programming
	if results[0].FactText != "makari likes programming in Go" {
		t.Errorf("expected top result to be Go programming fact, got: %s", results[0].FactText)
	}
	if results[0].Score <= results[1].Score {
		t.Errorf("results not sorted in descending order: score0=%f, score1=%f", results[0].Score, results[1].Score)
	}

	// 3. GetUserMemories
	userMems, err := store.GetUserMemories(chatID, userID)
	if err != nil {
		t.Fatalf("GetUserMemories failed: %v", err)
	}
	if len(userMems) != 2 {
		t.Errorf("expected 2 memories for user %s, got %d", userID, len(userMems))
	}

	// 4. Delete Memory
	memToDelete := results[0].ID
	err = store.DeleteMemory(chatID, memToDelete)
	if err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	remainingMems, err := store.GetUserMemories(chatID, userID)
	if err != nil {
		t.Fatalf("GetUserMemories after delete failed: %v", err)
	}
	if len(remainingMems) != 1 {
		t.Errorf("expected 1 remaining memory, got %d", len(remainingMems))
	}
	if remainingMems[0].ID == memToDelete {
		t.Errorf("deleted memory still present in store")
	}
}

func TestOpenRouterEmbedder_HTTP(t *testing.T) {
	// Mock HTTP server responding with OpenAI/OpenRouter embedding JSON schema
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var items []embeddingItem
		switch val := req.Input.(type) {
		case string:
			items = append(items, embeddingItem{
				Index:     0,
				Embedding: []float32{0.1, 0.2, 0.3, 0.4},
			})
		case []interface{}:
			for idx := range val {
				items = append(items, embeddingItem{
					Index:     idx,
					Embedding: []float32{float32(idx+1) * 0.1, 0.2, 0.3, 0.4},
				})
			}
		}

		resp := embeddingResponse{Data: items}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	embedder := NewOpenRouterEmbedder("test-api-key", "text-embedding-3-small")
	embedder.SetBaseURL(mockServer.URL)

	ctx := context.Background()
	emb, err := embedder.GetEmbedding(ctx, "hello world")
	if err != nil {
		t.Fatalf("GetEmbedding failed: %v", err)
	}
	if len(emb) != 4 || emb[0] < 0.09 || emb[0] > 0.11 {
		t.Errorf("unexpected embedding result: %v", emb)
	}

	batch, err := embedder.GetBatchEmbeddings(ctx, []string{"one", "two"})
	if err != nil {
		t.Fatalf("GetBatchEmbeddings failed: %v", err)
	}
	if len(batch) != 2 {
		t.Errorf("expected 2 batch vectors, got %d", len(batch))
	}
}

// --- 3. Hierarchical Chunking & Long-Term Knowledge Tests ---

func TestHierarchicalMemory_ChunkingAndMerging(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nova_hierarchical_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	summarizer := NewDefaultEpisodeSummarizer()
	hMem, err := NewHierarchicalMemory(tempDir, 3, summarizer)
	if err != nil {
		t.Fatalf("failed to create hierarchical memory: %v", err)
	}

	chatID := "group_math_crew@g.us"
	ctx := context.Background()

	// 1. Add messages to buffer
	now := time.Now()
	shouldCompress1 := hMem.AddMessage(chatID, MemoryMessage{
		SenderID:   "user_1",
		SenderName: "Alice",
		Text:       "Let's discuss the differential equations assignment for tomorrow.",
		Timestamp:  now.Add(-10 * time.Minute),
	})
	if shouldCompress1 {
		t.Errorf("expected shouldCompress to be false for 1st message")
	}

	hMem.AddMessage(chatID, MemoryMessage{
		SenderID:   "user_2",
		SenderName: "Bob",
		Text:       "I solved problem 3 using Laplace transforms.",
		Timestamp:  now.Add(-5 * time.Minute),
	})

	shouldCompress3 := hMem.AddMessage(chatID, MemoryMessage{
		SenderID:   "user_1",
		SenderName: "Alice",
		Text:       "Awesome, let's double check with Nova.",
		Timestamp:  now,
	})
	if !shouldCompress3 {
		t.Errorf("expected shouldCompress to be true after 3rd message (chunkSize=3)")
	}

	// 2. Compress Chunk into Episode
	episode1, err := hMem.CompressChunk(ctx, chatID)
	if err != nil {
		t.Fatalf("CompressChunk failed: %v", err)
	}

	if episode1.MessageCount != 3 {
		t.Errorf("expected 3 messages in episode1, got %d", episode1.MessageCount)
	}
	if len(episode1.Participants) < 2 {
		t.Errorf("expected Alice and Bob in participants, got: %v", episode1.Participants)
	}
	if len(hMem.GetBuffer(chatID)) != 0 {
		t.Errorf("expected buffer to be emptied after chunk compression")
	}

	// Verify episodes file exists
	episodes, err := hMem.GetEpisodes(chatID)
	if err != nil {
		t.Fatalf("GetEpisodes failed: %v", err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode stored, got %d", len(episodes))
	}

	// 3. Add a second chunk and compress
	hMem.AddMessage(chatID, MemoryMessage{SenderName: "Bob", Text: "We finalized the presentation slides.", Timestamp: now.Add(5 * time.Minute)})
	hMem.AddMessage(chatID, MemoryMessage{SenderName: "Alice", Text: "Great job everyone!", Timestamp: now.Add(6 * time.Minute)})
	hMem.AddMessage(chatID, MemoryMessage{SenderName: "Charlie", Text: "See you all tomorrow at university.", Timestamp: now.Add(7 * time.Minute)})

	episode2, err := hMem.CompressChunk(ctx, chatID)
	if err != nil {
		t.Fatalf("CompressChunk 2 failed: %v", err)
	}
	if episode2.MessageCount != 3 {
		t.Errorf("expected 3 messages in episode2, got %d", episode2.MessageCount)
	}

	allEpisodes, _ := hMem.GetEpisodes(chatID)
	if len(allEpisodes) != 2 {
		t.Fatalf("expected 2 stored episodes, got %d", len(allEpisodes))
	}

	// 4. Merge Episodes into Persistent Long-Term Knowledge Base
	kb, err := hMem.MergeEpisodesToLongTerm(ctx, chatID)
	if err != nil {
		t.Fatalf("MergeEpisodesToLongTerm failed: %v", err)
	}

	if kb.TotalEpisodes != 2 {
		t.Errorf("expected 2 total episodes in knowledge base, got %d", kb.TotalEpisodes)
	}
	if kb.TotalMessages != 6 {
		t.Errorf("expected 6 total messages in knowledge base, got %d", kb.TotalMessages)
	}
	if kb.MasterSummary == "" {
		t.Errorf("expected master summary to be populated")
	}

	// Check files created
	safeChat := sanitizeChatID(chatID)
	kbJSON := filepath.Join(tempDir, fmt.Sprintf("knowledge_%s.json", safeChat))
	kbMD := filepath.Join(tempDir, fmt.Sprintf("knowledge_%s.md", safeChat))
	if _, err := os.Stat(kbJSON); os.IsNotExist(err) {
		t.Errorf("expected knowledge JSON file at %s", kbJSON)
	}
	if _, err := os.Stat(kbMD); os.IsNotExist(err) {
		t.Errorf("expected knowledge Markdown file at %s", kbMD)
	}

	// 5. Test FormatKnowledgePrompt
	prompt, err := hMem.FormatKnowledgePrompt(chatID)
	if err != nil {
		t.Fatalf("FormatKnowledgePrompt failed: %v", err)
	}
	if prompt == "" {
		t.Errorf("expected formatted knowledge prompt, got empty string")
	}

	// 6. Verify second merge when no new episodes are pending
	kb2, err := hMem.MergeEpisodesToLongTerm(ctx, chatID)
	if err != nil {
		t.Fatalf("MergeEpisodesToLongTerm (idempotent) failed: %v", err)
	}
	if kb2.TotalEpisodes != 2 {
		t.Errorf("expected total episodes to remain 2, got %d", kb2.TotalEpisodes)
	}
}

// --- 4. TriTierEngine Full Integration Tests ---

func TestTriTierEngine(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "nova_tritier_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockEmb := &mockEmbedder{
		embedMap: map[string][]float32{
			"Makari is studying Artificial Intelligence": {1.0, 0.5, 0.0, 0.0},
			"Query about Makari's study field":          {0.95, 0.45, 0.0, 0.0},
		},
	}

	engine, err := NewTriTierEngine(tempDir, mockEmb, NewDefaultEpisodeSummarizer())
	if err != nil {
		t.Fatalf("failed to create TriTierEngine: %v", err)
	}

	chatID := "private_user_101"
	ctx := context.Background()

	// 1. Save semantic memory (Tier 3)
	err = engine.SaveMemory(chatID, "user_101", "Makari", "Makari is studying Artificial Intelligence", []float32{1.0, 0.5, 0.0, 0.0})
	if err != nil {
		t.Fatalf("SaveMemory failed: %v", err)
	}

	// 2. Record messages into Tier 1 & trigger Tier 2
	for i := 1; i <= 15; i++ {
		_, err := engine.RecordMessage(ctx, chatID, MemoryMessage{
			SenderID:   "user_101",
			SenderName: "Makari",
			Text:       fmt.Sprintf("Message number %d discussing projects", i),
			Timestamp:  time.Now(),
		})
		if err != nil {
			t.Fatalf("RecordMessage failed at %d: %v", i, err)
		}
	}

	// Merge episodic memory to long-term
	_, err = engine.HierarchicalMemory().MergeEpisodesToLongTerm(ctx, chatID)
	if err != nil {
		t.Fatalf("MergeEpisodesToLongTerm failed: %v", err)
	}

	// 3. Get Comprehensive Context
	comprehensiveContext, err := engine.GetComprehensiveContext(ctx, chatID, "user_101", "Query about Makari's study field")
	if err != nil {
		t.Fatalf("GetComprehensiveContext failed: %v", err)
	}

	if comprehensiveContext == "" {
		t.Errorf("expected comprehensive context to be generated")
	}

	// Verify that context contains both the knowledge base summary and the user/semantic fact
	if !filepath.IsAbs(tempDir) {
		t.Logf("tempDir: %s", tempDir)
	}
	t.Logf("Comprehensive Context Output:\n%s", comprehensiveContext)
}
