package voice

import (
	"context"
	"testing"
)

func TestNewGroqSTT(t *testing.T) {
	stt := NewGroqSTT("test-groq-key", "")
	if stt.model != "whisper-large-v3" {
		t.Errorf("expected default model whisper-large-v3, got %s", stt.model)
	}

	sttCustom := NewGroqSTT("test-key", "custom-whisper")
	if sttCustom.model != "custom-whisper" {
		t.Errorf("expected custom-whisper, got %s", sttCustom.model)
	}
}

func TestTranscribeAudioValidation(t *testing.T) {
	sttNoKey := NewGroqSTT("", "")
	_, err := sttNoKey.TranscribeAudio(context.Background(), []byte("audio-bytes"), "")
	if err == nil {
		t.Errorf("expected error for empty API key")
	}

	stt := NewGroqSTT("test-key", "")
	_, err = stt.TranscribeAudio(context.Background(), nil, "")
	if err == nil {
		t.Errorf("expected error for nil audio bytes")
	}
}
