package image

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerator_DataURIExtraction(t *testing.T) {
	mockBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	b64 := base64.StdEncoding.EncodeToString(mockBytes)
	content := "Here is your picture:\n![generated](data:image/png;base64," + b64 + ")\nEnjoy!"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatCompletionResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		}{
			Message: struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{
				Role:    "assistant",
				Content: content,
			},
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gen := NewGenerator("test-key", server.URL, "gpt-image-2.5-sunburst")
	data, mime, cost, err := gen.GenerateImage(context.Background(), "draw an apple")
	if err != nil {
		t.Fatalf("GenerateImage failed: %v", err)
	}
	if mime != "image/png" {
		t.Errorf("expected mime image/png, got %s", mime)
	}
	if len(data) != len(mockBytes) {
		t.Errorf("expected data len %d, got %d", len(mockBytes), len(data))
	}
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
}

func TestGenerator_SetConfig(t *testing.T) {
	gen := NewGenerator("key", "https://api.code.signor.ai/v1", "gpt-image-2.5-sunburst")
	b, m := gen.GetConfig()
	if b != "https://api.code.signor.ai/v1" || m != "gpt-image-2.5-sunburst" {
		t.Errorf("unexpected initial config: %s, %s", b, m)
	}

	gen.SetConfig("https://api.code.signor.ai/api-2/v1", "gpt-image-2.5-sunburst-c")
	b, m = gen.GetConfig()
	if b != "https://api.code.signor.ai/api-2/v1" || m != "gpt-image-2.5-sunburst-c" {
		t.Errorf("unexpected updated config: %s, %s", b, m)
	}
}
