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

func sanitizeOptionalField(ptr *string) *string {
	if ptr == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*ptr)
	if trimmed == "" {
		return nil
	}
	lower := strings.ToLower(trimmed)
	switch lower {
	case "null", "none", "nil", "- null", "-null", "لا يوجد", "مافيش":
		return nil
	}
	return &trimmed
}

func splitBubbles(text string) []string {
	parts := strings.Split(text, "||")
	var bubbles []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			bubbles = append(bubbles, t)
		}
	}
	return bubbles
}

// ParseResponse parses and validates the response from the LLM.
// It seamlessly supports structured JSON payloads as well as direct natural conversational text.
func ParseResponse(raw string) (*ResponsePayload, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty response from model")
	}

	jsonStr := ExtractJSON(trimmed)
	if strings.HasPrefix(jsonStr, "{") && strings.HasSuffix(jsonStr, "}") {
		var resp ResponsePayload
		if err := json.Unmarshal([]byte(jsonStr), &resp); err == nil {
			resp.MemoryNote = sanitizeOptionalField(resp.MemoryNote)
			resp.ReactionEmoji = sanitizeOptionalField(resp.ReactionEmoji)

			// Clean bubbles if provided
			if len(resp.Bubbles) > 0 {
				cleaned := make([]string, 0, len(resp.Bubbles))
				for _, b := range resp.Bubbles {
					if tb := strings.TrimSpace(b); tb != "" {
						cleaned = append(cleaned, tb)
					}
				}
				resp.Bubbles = cleaned
			}

			if len(resp.Bubbles) > 0 && (resp.ReplyText == nil || strings.TrimSpace(*resp.ReplyText) == "") {
				joined := strings.Join(resp.Bubbles, " || ")
				resp.ReplyText = &joined
			}

			if resp.ReplyText != nil && strings.Contains(*resp.ReplyText, "||") && len(resp.Bubbles) == 0 {
				resp.Bubbles = splitBubbles(*resp.ReplyText)
			}

			actionLower := strings.ToLower(strings.TrimSpace(resp.Action))
			if actionLower == "ignore" {
				resp.Action = "ignore"
				resp.ShouldReply = false
			} else if actionLower == "react_only" && resp.ReactionEmoji != nil {
				resp.Action = "react_only"
				resp.ShouldReply = false
			}

			if resp.ShouldReply {
				if resp.ReplyText != nil && strings.TrimSpace(*resp.ReplyText) != "" {
					if resp.Action == "" {
						resp.Action = "reply"
					}
					return &resp, nil
				}
			} else {
				return &resp, nil
			}
		}
	}

	// Plain-text fallback:
	upper := strings.ToUpper(trimmed)
	if upper == "[IGNORE]" || upper == "[SILENT]" {
		return &ResponsePayload{
			ShouldReply: false,
			Action:      "ignore",
		}, nil
	}

	var reactionEmoji *string
	if strings.HasPrefix(upper, "[REACT:") {
		if closeIdx := strings.Index(trimmed, "]"); closeIdx != -1 {
			emoji := strings.TrimSpace(trimmed[len("[REACT:"):closeIdx])
			if emoji != "" {
				reactionEmoji = &emoji
			}
			remaining := strings.TrimSpace(trimmed[closeIdx+1:])
			if remaining == "" {
				return &ResponsePayload{
					ShouldReply:   false,
					Action:        "react_only",
					ReactionEmoji: reactionEmoji,
				}, nil
			}
			trimmed = remaining
		}
	}

	var bubbles []string
	if strings.Contains(trimmed, "||") {
		bubbles = splitBubbles(trimmed)
	}

	mood := "neutral"
	return &ResponsePayload{
		ShouldReply:   true,
		Action:        "reply",
		ReplyText:     &trimmed,
		Bubbles:       bubbles,
		Mood:          &mood,
		ReactionEmoji: reactionEmoji,
	}, nil
}
