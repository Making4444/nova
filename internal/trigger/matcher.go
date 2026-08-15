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
	var b strings.Builder
	b.Grow(len(res))

	for _, r := range res {
		if isArabicDiacriticOrTatweel(r) {
			continue
		}

		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			b.WriteRune('ا')
		case 'ة':
			b.WriteRune('ه')
		case 'ى':
			b.WriteRune('ي')
		case 'ؤ', 'ئ':
			b.WriteRune('ء')
		default:
			// Strip punctuation and special symbols from matching copy, replace with space
			if unicode.IsPunct(r) || unicode.IsSymbol(r) {
				b.WriteRune(' ')
			} else {
				b.WriteRune(r)
			}
		}
	}

	// 3. Collapse consecutive whitespace and trim
	return strings.TrimSpace(whitespaceRegex.ReplaceAllString(b.String(), " "))
}

// ContainsTrigger checks if the normalized copy contains "يا نوفا" or "يانوفا" (or "يا نوفه" / "يانوفه").
func ContainsTrigger(text string) bool {
	if text == "" {
		return false
	}

	norm := NormalizeForMatch(text)

	// Check for "يا نوفا" or concatenated "يانوفا" (and "يا نوفه" / "يانوفه")
	if strings.Contains(norm, "يا نوفا") || strings.Contains(norm, "يانوفا") ||
		strings.Contains(norm, "يا نوفه") || strings.Contains(norm, "يانوفه") {
		return true
	}

	return false
}

// CheckTrigger checks if the trigger "يا نوفا" is matched in either:
// 1. The current message text
// 2. The replied-to message text (if this is a reply)
func CheckTrigger(messageText string, isReply bool, repliedToText string) bool {
	if ContainsTrigger(messageText) {
		return true
	}

	if isReply && ContainsTrigger(repliedToText) {
		return true
	}

	return false
}
