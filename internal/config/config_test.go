package config

import (
	"os"
	"testing"
)

func TestLoadConfig_SignorAI(t *testing.T) {
	// Setup environment variables
	os.Setenv("SIGNOR_API_KEY", "sk-signor-test-key-123")
	os.Setenv("SIGNOR_CHAT_BASE_URL", "https://api.code.signor.ai/v1")
	os.Setenv("SIGNOR_CHAT_MODEL", "gpt-6-luna")
	os.Setenv("SIGNOR_GEMINI_BASE_URL", "https://api.code.signor.ai/api-4/v1")
	os.Setenv("SIGNOR_TTS_BASE_URL", "https://api.code.signor.ai/api-2/v1")
	os.Setenv("SIGNOR_TTS_MODEL", "gemini-3.8-flash-tts")
	os.Setenv("SYSTEM_PROMPT_PATH", "../../config/system_prompt.md")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.SignorAPIKey != "sk-signor-test-key-123" {
		t.Errorf("expected SignorAPIKey 'sk-signor-test-key-123', got %q", cfg.SignorAPIKey)
	}
	if cfg.SignorChatBaseURL != "https://api.code.signor.ai/v1" {
		t.Errorf("expected SignorChatBaseURL 'https://api.code.signor.ai/v1', got %q", cfg.SignorChatBaseURL)
	}
	if cfg.SignorChatModel != "gpt-6-luna" {
		t.Errorf("expected SignorChatModel 'gpt-6-luna', got %q", cfg.SignorChatModel)
	}
	if cfg.SignorGeminiBaseURL != "https://api.code.signor.ai/api-4/v1" {
		t.Errorf("expected SignorGeminiBaseURL 'https://api.code.signor.ai/api-4/v1', got %q", cfg.SignorGeminiBaseURL)
	}
	if cfg.SignorTTSBaseURL != "https://api.code.signor.ai/api-2/v1" {
		t.Errorf("expected SignorTTSBaseURL 'https://api.code.signor.ai/api-2/v1', got %q", cfg.SignorTTSBaseURL)
	}
	if cfg.SignorTTSModel != "gemini-3.8-flash-tts" {
		t.Errorf("expected SignorTTSModel 'gemini-3.8-flash-tts', got %q", cfg.SignorTTSModel)
	}
	if cfg.ModelChat != "gpt-6-luna" {
		t.Errorf("expected ModelChat 'gpt-6-luna', got %q", cfg.ModelChat)
	}
	if cfg.ModelMath != "gemini-3.8-flash" {
		t.Errorf("expected ModelMath 'gemini-3.8-flash', got %q", cfg.ModelMath)
	}
	if cfg.TTSModel != "gemini-3.8-flash-tts" {
		t.Errorf("expected TTSModel to default to 'gemini-3.8-flash-tts', got %q", cfg.TTSModel)
	}
	if cfg.TTSVoice != "alloy" {
		t.Errorf("expected TTSVoice to default to 'alloy' when Signor is configured, got %q", cfg.TTSVoice)
	}
}
