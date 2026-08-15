package ai

import (
	"encoding/json"
	"errors"
	"fmt"
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

// ParseResponse parses and validates the JSON response from the LLM.
func ParseResponse(raw string) (*ResponsePayload, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, errors.New("empty response from model")
	}

	jsonStr := ExtractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON object found in response: %q", raw)
	}

	var resp ResponsePayload
	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON response (%w): raw text: %q", err, raw)
	}

	if resp.ShouldReply {
		if resp.ReplyText == nil || strings.TrimSpace(*resp.ReplyText) == "" {
			return nil, errors.New("model indicated should_reply: true but reply_text is missing or empty")
		}
	}

	return &resp, nil
}
