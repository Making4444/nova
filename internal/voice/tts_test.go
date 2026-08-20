package voice

import (
	"context"
	"testing"
)

func TestNewOpenRouterTTS(t *testing.T) {
	tts := NewOpenRouterTTS("test-key", "", "")
	if tts.model != "openai/gpt-audio-mini" {
		t.Errorf("expected model openai/gpt-audio-mini, got %s", tts.model)
	}
	if tts.voice != "onyx" {
		t.Errorf("expected voice onyx, got %s", tts.voice)
	}
}

func TestSynthesizeEmptyText(t *testing.T) {
	tts := NewOpenRouterTTS("test-key", "openai/gpt-audio-mini", "onyx")
	_, _, err := tts.SynthesizeToOggOpus(context.Background(), "   ")
	if err == nil {
		t.Errorf("expected error for empty text, got nil")
	}
}
