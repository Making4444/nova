package games

import (
	"strings"
	"unicode"
)

// NormalizeArabic cleans Arabic text for fuzzy matching by removing diacritics,
// unifying letter forms (أ/إ/آ -> ا, ة -> ه, ى -> ي), removing punctuation, and collapsing whitespace.
func NormalizeArabic(text string) string {
	if text == "" {
		return ""
	}

	var sb strings.Builder
	var lastRune rune
	var prevRune rune
	var runCount int

	for _, r := range text {
		// 1. Strip Tashkeel & Diacritics (0x064B - 0x065F, 0x0670, Tatweel 0x0640)
		if (r >= 0x064B && r <= 0x065F) || r == 0x0670 || r == 0x0640 {
			continue
		}

		// 2. Normalize Alefs
		switch r {
		case 'أ', 'إ', 'آ', 'ٱ':
			r = 'ا'
		case 'ة':
			r = 'ه'
		case 'ى':
			r = 'ي'
		case 'ؤ':
			r = 'و'
		case 'ئ':
			r = 'ي'
		}

		// 3. Normalize Arabic / Persian digits to standard ASCII digits
		switch {
		case r >= '٠' && r <= '٩':
			r = '0' + (r - '٠')
		case r >= '۰' && r <= '۹':
			r = '0' + (r - '۰')
		}

		// 4. Remove punctuation, symbols, emojis
		if unicode.IsPunct(r) || unicode.IsSymbol(r) || unicode.IsMark(r) {
			r = ' '
		}

		if unicode.IsSpace(r) {
			if lastRune != ' ' && sb.Len() > 0 {
				sb.WriteRune(' ')
				prevRune = lastRune
				lastRune = ' '
				runCount = 0
			}
			continue
		}

		lowerR := unicode.ToLower(r)
		if lowerR == lastRune {
			// In standard Arabic, the only letter that legally appears doubled is 'ل' right after 'ا' (as in "الـلـ...")
			isLegalDoubleLam := (lowerR == 'ل' && prevRune == 'ا' && runCount == 1)
			if isLegalDoubleLam {
				runCount++
			} else {
				continue
			}
		} else {
			prevRune = lastRune
			lastRune = lowerR
			runCount = 1
		}

		sb.WriteRune(lowerR)
	}

	return strings.TrimSpace(sb.String())
}

// StripCommonPrefixes removes conversational prefixes like "فيلم", "هو", "هي", "ده", "دي"
func StripCommonPrefixes(s string) string {
	s = strings.TrimSpace(s)
	prefixes := []string{
		"فيلم ",
		"مسلسل ",
		"اسم الفيلم ",
		"هو ",
		"هي ",
		"ده ",
		"دي ",
		"الاجابه ",
		"الاجابة ",
		"حلها ",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			s = strings.TrimSpace(strings.TrimPrefix(s, p))
		}
	}
	return s
}

// CheckAnswer compares a user candidate answer against the accepted answer variations.
// Returns true if an acceptable match is found.
func CheckAnswer(userText string, acceptedAnswers []string) bool {
	normUser := NormalizeArabic(userText)
	if normUser == "" {
		return false
	}
	strippedUser := StripCommonPrefixes(normUser)

	for _, accepted := range acceptedAnswers {
		normAccepted := NormalizeArabic(accepted)
		if normAccepted == "" {
			continue
		}
		strippedAccepted := StripCommonPrefixes(normAccepted)

		// 1. Direct match (normalized)
		if normUser == normAccepted || strippedUser == strippedAccepted {
			return true
		}

		// 2. User text matches with "ال" stripped or added
		if strings.TrimPrefix(strippedUser, "ال") == strings.TrimPrefix(strippedAccepted, "ال") {
			return true
		}

		// 3. User response contains the exact target answer as a distinct token sequence
		// e.g. "فيلم الناظر طبعا" contains "الناظر"
		if len(strippedAccepted) >= 3 && strings.Contains(strippedUser, strippedAccepted) {
			return true
		}
		if len(normAccepted) >= 3 && strings.Contains(normUser, normAccepted) {
			return true
		}

		// 4. Fuzzy Levenshtein Distance for minor typos (e.g. 1 letter typo)
		if isFuzzyMatch(strippedUser, strippedAccepted) {
			return true
		}
	}

	return false
}

// isFuzzyMatch checks if two strings match within an acceptable error tolerance.
func isFuzzyMatch(s1, s2 string) bool {
	l1 := len([]rune(s1))
	l2 := len([]rune(s2))
	if l1 < 3 || l2 < 3 {
		return false
	}

	diff := l1 - l2
	if diff < 0 {
		diff = -diff
	}
	// If length difference is more than 2 runes, don't fuzzy match
	if diff > 2 {
		return false
	}

	dist := levenshteinDistance([]rune(s1), []rune(s2))
	maxLen := l1
	if l2 > maxLen {
		maxLen = l2
	}

	// For short words (3-5 chars), max 1 typo allowed
	if maxLen <= 5 && dist <= 1 {
		return true
	}
	// For longer words (6+ chars), up to 2 typos or similarity >= 0.78
	if maxLen > 5 && dist <= 2 {
		return true
	}

	return false
}

// levenshteinDistance computes the minimum edit distance between two rune slices.
func levenshteinDistance(r1, r2 []rune) int {
	len1 := len(r1)
	len2 := len(r2)

	column := make([]int, len1+1)
	for y := 1; y <= len1; y++ {
		column[y] = y
	}

	for x := 1; x <= len2; x++ {
		column[0] = x
		lastkey := x - 1
		for y := 1; y <= len1; y++ {
			oldkey := column[y]
			cost := 0
			if r1[y-1] != r2[x-1] {
				cost = 1
			}
			min := lastkey + cost
			if column[y-1]+1 < min {
				min = column[y-1] + 1
			}
			if column[y]+1 < min {
				min = column[y] + 1
			}
			column[y] = min
			lastkey = oldkey
		}
	}

	return column[len1]
}
