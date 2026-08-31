package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config holds all configuration parameters for Nova Bot.
type Config struct {
	OpenRouterAPIKey    string
	ModelChat           string
	ModelMath           string
	ModelVision         string
	GroqAPIKey          string
	ModelWhisper        string
	ModelRouterGroq     string
	OpenRouterModel     string // Default chat model fallback
	SystemPrompt        string
	SessionDBPath       string
	ChatCooldownSeconds int
	ContextHistoryLimit int
	TTSModel            string
	TTSVoice            string
}

// LoadConfig loads environment variables and reads the system prompt once on startup.
func LoadConfig() (*Config, error) {
	// Attempt to load .env file; do not fail if missing as env vars might be passed directly.
	_ = godotenv.Load()

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	groqKey := os.Getenv("GROQ_API_KEY")

	modelChat := os.Getenv("MODEL_CHAT")
	if modelChat == "" {
		modelChat = os.Getenv("OPENROUTER_MODEL")
		if modelChat == "" {
			modelChat = "qwen/qwen3-235b-a22b-2507"
		}
	}

	modelMath := os.Getenv("MODEL_MATH")
	if modelMath == "" {
		modelMath = "z-ai/glm-5.2"
	}

	modelVision := os.Getenv("MODEL_VISION")
	if modelVision == "" {
		modelVision = "openai/gpt-5.6-luna"
	}

	modelWhisper := os.Getenv("MODEL_WHISPER")
	if modelWhisper == "" {
		modelWhisper = "whisper-large-v3"
	}

	modelRouterGroq := os.Getenv("MODEL_ROUTER_GROQ")
	if modelRouterGroq == "" {
		modelRouterGroq = "qwen/qwen3.8-27b"
	}

	promptPath := os.Getenv("SYSTEM_PROMPT_PATH")
	if promptPath == "" {
		promptPath = "config/system_prompt.md"
	}

	promptBytes, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read system prompt from %s: %w", promptPath, err)
	}

	sessionDB := os.Getenv("SESSION_DB_PATH")
	if sessionDB == "" {
		sessionDB = "data/session.db"
	}

	cooldownSec := 3
	if cdStr := os.Getenv("CHAT_COOLDOWN_SECONDS"); cdStr != "" {
		if v, err := strconv.Atoi(cdStr); err == nil && v >= 0 {
			cooldownSec = v
		}
	}

	// Default is 0 (all messages in the conversation)
	historyLimit := 0
	if hlStr := os.Getenv("CONTEXT_HISTORY_LIMIT"); hlStr != "" {
		if strings.ToLower(strings.TrimSpace(hlStr)) != "all" {
			if v, err := strconv.Atoi(hlStr); err == nil && v >= 0 {
				historyLimit = v
			}
		}
	}

	// TTS Configuration (OpenRouter Audio / Speech)
	ttsModel := os.Getenv("TTS_MODEL")
	if ttsModel == "" {
		ttsModel = "openai/gpt-audio-mini"
	}

	ttsVoice := os.Getenv("TTS_VOICE")
	if ttsVoice == "" {
		ttsVoice = "onyx" // Male voice for Nova (onyx / echo / alloy)
	}

	return &Config{
		OpenRouterAPIKey:    apiKey,
		ModelChat:           modelChat,
		ModelMath:           modelMath,
		ModelVision:         modelVision,
		GroqAPIKey:          groqKey,
		ModelWhisper:        modelWhisper,
		ModelRouterGroq:     modelRouterGroq,
		OpenRouterModel:     modelChat,
		SystemPrompt:        string(promptBytes),
		SessionDBPath:       sessionDB,
		ChatCooldownSeconds: cooldownSec,
		ContextHistoryLimit: historyLimit,
		TTSModel:            ttsModel,
		TTSVoice:            ttsVoice,
	}, nil
}
