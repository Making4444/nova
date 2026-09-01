package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// TriTierEngine coordinates all three tiers of memory:
// - Tier 1: Working buffer / Short-term context
// - Tier 2: Episodic & Hierarchical chunk summaries + Persistent Knowledge Base
// - Tier 3: Semantic vector fact memory with Cosine Similarity search
type TriTierEngine struct {
	vectorStore  *VectorStore
	hierarchical *HierarchicalMemory
	mu           sync.RWMutex
}

// NewTriTierEngine initializes the full Tri-Tier memory engine.
func NewTriTierEngine(baseDir string, embedder Embedder, summarizer EpisodeSummarizer) (*TriTierEngine, error) {
	if baseDir == "" {
		baseDir = "data/memory"
	}

	vStore, err := NewVectorStore(baseDir, embedder)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize vector store: %w", err)
	}

	hMem, err := NewHierarchicalMemory(baseDir, 15, summarizer)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize hierarchical memory: %w", err)
	}

	return &TriTierEngine{
		vectorStore:  vStore,
		hierarchical: hMem,
	}, nil
}

// VectorStore returns the underlying Tier 3 VectorStore.
func (e *TriTierEngine) VectorStore() *VectorStore {
	if e == nil {
		return nil
	}
	return e.vectorStore
}

// HierarchicalMemory returns the underlying Tier 2 HierarchicalMemory manager.
func (e *TriTierEngine) HierarchicalMemory() *HierarchicalMemory {
	if e == nil {
		return nil
	}
	return e.hierarchical
}

// SaveMemory saves a semantic fact into Tier 3 vector memory.
func (e *TriTierEngine) SaveMemory(chatID, userID, userName, factText string, embedding []float32) error {
	if e == nil || e.vectorStore == nil {
		return nil
	}
	return e.vectorStore.SaveMemory(chatID, userID, userName, factText, embedding)
}

// SearchRelevantMemories searches Tier 3 vector memory using queryEmbedding.
func (e *TriTierEngine) SearchRelevantMemories(chatID string, queryEmbedding []float32, topK int, minScore float32) ([]MemoryItem, error) {
	if e == nil || e.vectorStore == nil {
		return []MemoryItem{}, nil
	}
	return e.vectorStore.SearchRelevantMemories(chatID, queryEmbedding, topK, minScore)
}

// DeleteMemory deletes a memory item from Tier 3 vector memory.
func (e *TriTierEngine) DeleteMemory(chatID string, memoryID string) error {
	if e == nil || e.vectorStore == nil {
		return nil
	}
	return e.vectorStore.DeleteMemory(chatID, memoryID)
}

// GetUserMemories retrieves all Tier 3 memories for a user.
func (e *TriTierEngine) GetUserMemories(chatID, userID string) ([]MemoryItem, error) {
	if e == nil || e.vectorStore == nil {
		return []MemoryItem{}, nil
	}
	return e.vectorStore.GetUserMemories(chatID, userID)
}

// RecordMessage adds a message into Tier 1 buffer and automatically triggers Tier 2 episode compression if threshold is met.
func (e *TriTierEngine) RecordMessage(ctx context.Context, chatID string, msg MemoryMessage) (*EpisodeSummary, error) {
	if e == nil || e.hierarchical == nil {
		return nil, nil
	}
	shouldCompress := e.hierarchical.AddMessage(chatID, msg)
	if shouldCompress {
		return e.hierarchical.CompressChunk(ctx, chatID)
	}
	return nil, nil
}

// GetComprehensiveContext aggregates Tier 2 Knowledge Base + Tier 3 Semantic Memories into a Markdown block.
func (e *TriTierEngine) GetComprehensiveContext(ctx context.Context, chatID, userID, queryText string) (string, error) {
	if e == nil {
		return "", nil
	}
	var sections []string

	// 1. Tier 2: Persistent Knowledge Base
	if e.hierarchical != nil {
		kbPrompt, err := e.hierarchical.FormatKnowledgePrompt(chatID)
		if err == nil && kbPrompt != "" {
			sections = append(sections, kbPrompt)
		}
	}

	// 2. Tier 3: Semantic Vector Memories (if query text is provided and embedder exists)
	if e.vectorStore != nil && queryText != "" && e.vectorStore.embedder != nil {
		memories, err := e.vectorStore.SearchByText(ctx, chatID, queryText, 5, 0.45)
		if err == nil && len(memories) > 0 {
			var b strings.Builder
			b.WriteString("### 🔍 ذكريات ومعلومات ذات صلة (Relevant Semantic Memories):\n")
			for _, m := range memories {
				sender := m.UserName
				if sender == "" {
					sender = m.UserID
				}
				if sender != "" {
					b.WriteString(fmt.Sprintf("- [%s]: %s (درجة التطابق: %.2f)\n", sender, m.FactText, m.Score))
				} else {
					b.WriteString(fmt.Sprintf("- %s (درجة التطابق: %.2f)\n", m.FactText, m.Score))
				}
			}
			sections = append(sections, strings.TrimSpace(b.String()))
		}
	}

	// 3. User Specific Facts (if userID given)
	if e.vectorStore != nil && userID != "" {
		userMems, err := e.vectorStore.GetUserMemories(chatID, userID)
		if err == nil && len(userMems) > 0 {
			var b strings.Builder
			b.WriteString(fmt.Sprintf("### 👤 حقائق مسجلة عن المستخدم (%s):\n", userID))
			for _, m := range userMems {
				b.WriteString(fmt.Sprintf("- %s\n", m.FactText))
			}
			sections = append(sections, strings.TrimSpace(b.String()))
		}
	}

	return strings.Join(sections, "\n\n"), nil
}
