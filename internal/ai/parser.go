package ai

import (
	"encoding/json"
	"errors"
	"strings"
)

// ExtractJSON attempts to clean and extract the raw JSON object string from LLM output.
func ExtractJSON(raw string) string {
	trimmed := strings.TrimSpace(raw)

	// Remove markdown code fences if present (e.g. ```json ... ``` or ``` ... ```)
	if strings.HasPrefix(trimmed, "```") {
		// Strip first line (```json or ```)
		if idx := strings.Index(trimmed, "\n"); idx != -1 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		}
		// Strip trailing ```
		if strings.HasSuffix(trimmed, "```") {
			trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, "```"))
		}
	}

	// Find the outermost '{' and '}'
	startIdx := strings.Index(trimmed, "{")
	endIdx := strings.LastIndex(trimmed, "}")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		trimmed = trimmed[startIdx : endIdx+1]
	}

	return strings.TrimSpace(trimmed)
}

// ParseResponse parses and validates the response from the LLM.
// It seamlessly supports structured JSON payloads as well as direct natural conversational text.
func ParseResponse(raw string) (*ResponsePayload, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty response from model")
	}

	jsonStr := ExtractJSON(trimmed)
	if jsonStr != "" {
		var resp ResponsePayload
		if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil {
			if resp.ShouldReply {
				if resp.ReplyText != nil && strings.TrimSpace(*resp.ReplyText) != "" {
					return &resp, nil
				}
			} else {
				return &resp, nil
			}
		}
	}

	// Seamless fallback for direct conversational text:
	mood := "neutral"
	return &ResponsePayload{
		ShouldReply: true,
		ReplyText:   &trimmed,
		Mood:        &mood,
	}, nil
}
