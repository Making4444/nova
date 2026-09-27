package trigger

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// regex to collapse multiple whitespace characters into a single space
	whitespaceRegex = regexp.MustCompile(`\s+`)
)

func isArabicDiacriticOrTatweel(r rune) bool {
	return (r >= 0x064B && r <= 0x065F) || r == 0x0670 || r == 0x0640
}

// NormalizeForMatch returns a normalized copy of the text specifically for trigger matching.
// IMPORTANT: This must NEVER be used to modify the original stored or sent text.
func NormalizeForMatch(s string) string {
	if s == "" {
		return ""
	}

	// 1. Convert to lowercase (for any Latin letters)
	res := strings.ToLower(s)

	// 2. Normalize Arabic characters and strip Tashkeel/Tatweel
	var mapped []rune
	for _, r := range res {
		if isArabicDiacriticOrTatweel(r) {
			continue
		}

		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			mapped = append(mapped, 'ا')
		case 'ة':
			mapped = append(mapped, 'ه')
		case 'ى':
			mapped = append(mapped, 'ي')
		case 'ؤ', 'ئ':
			mapped = append(mapped, 'ء')
		default:
			// Strip punctuation and special symbols from matching copy, replace with space
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				mapped = append(mapped, ' ')
			} else {
				mapped = append(mapped, r)
			}
		}
	}

	// 3. Collapse 3+ consecutive identical Arabic characters (e.g. "نووووفا" -> "نوفا", "ياااا" -> "يا")
	var b strings.Builder
	b.Grow(len(res))
	for i := 0; i < len(mapped); {
		r := mapped[i]
		j := i + 1
		for j < len(mapped) && mapped[j] == r {
			j++
		}
		runLen := j - i
		if r >= 0x0600 && r <= 0x06FF && runLen >= 3 {
			b.WriteRune(r)
		} else {
			for k := 0; k < runLen; k++ {
				b.WriteRune(r)
			}
		}
		i = j
	}

	// 4. Collapse consecutive whitespace and trim
	return strings.TrimSpace(whitespaceRegex.ReplaceAllString(b.String(), " "))
}

// ContainsTrigger checks if the normalized copy contains "يا نوفا", "يانوفا", or tag mentions like "@nova", "@نوفا".
// Standalone "نوفا" without "يا" or "@" is strictly excluded.
func ContainsTrigger(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}

	norm := NormalizeForMatch(text)
	lowerRaw := strings.ToLower(text)

	// Check explicit tags in raw text
	if strings.Contains(lowerRaw, "@nova") || strings.Contains(text, "@نوفا") || strings.Contains(text, "@201202172699") {
		return true
	}

	// Check for "يا نوفا" or concatenated "يانوفا" (and "يا نوفه" / "يانوفه")
	if strings.Contains(norm, "يا نوفا") || strings.Contains(norm, "يانوفا") ||
		strings.Contains(norm, "يا نوفه") || strings.Contains(norm, "يانوفه") {
		return true
	}

	return false
}

// CheckTrigger checks if the trigger is matched in either direct text or replied text.
func CheckTrigger(messageText string, isReply bool, repliedToText string) bool {
	return CheckTriggerWithMentions(messageText, isReply, repliedToText, nil, "")
}

// CheckTriggerWithMentions checks if the trigger is matched in text, reply chain, or WhatsApp @mentioned JIDs.
func CheckTriggerWithMentions(messageText string, isReply bool, repliedToText string, mentionedJIDs []string, botJID string) bool {
	if ContainsTrigger(messageText) {
		return true
	}

	if isReply && ContainsTrigger(repliedToText) {
		return true
	}

	// Check if bot's JID or number was mentioned in WhatsApp MentionedJID array
	if botJID != "" {
		cleanBot := strings.TrimSuffix(botJID, "@s.whatsapp.net")
		cleanBot = strings.TrimSuffix(cleanBot, "@lid")
		for _, m := range mentionedJIDs {
			if strings.Contains(m, cleanBot) || strings.Contains(m, "201202172699") {
				return true
			}
		}
	} else {
		for _, m := range mentionedJIDs {
			if strings.Contains(m, "201202172699") {
				return true
			}
		}
	}

	return false
}
