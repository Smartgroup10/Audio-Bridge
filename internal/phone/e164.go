package phone

import "strings"

// NormalizeE164 normalizes a phone number to E.164 format.
// Default country: Spain (+34). Non-numeric inputs pass through unchanged.
func NormalizeE164(number string) string {
	if number == "" {
		return ""
	}

	// Strip spaces, dashes, dots, parens
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, number)

	// Not a phone number (e.g., "anonymous", "unknown", "restricted")
	if cleaned == "" || (!isDigits(cleaned) && !strings.HasPrefix(cleaned, "+")) {
		return number
	}

	// Already E.164
	if strings.HasPrefix(cleaned, "+") {
		return cleaned
	}

	// Remove leading 00 (international dialing prefix)
	if strings.HasPrefix(cleaned, "00") {
		return "+" + cleaned[2:]
	}

	// Starts with 34 + 9 digits = Spanish number without +
	if strings.HasPrefix(cleaned, "34") && len(cleaned) == 11 {
		return "+" + cleaned
	}

	// 9-digit Spanish national number (6xx, 7xx, 8xx, 9xx)
	if len(cleaned) == 9 && (cleaned[0] >= '6' && cleaned[0] <= '9') {
		return "+34" + cleaned
	}

	// Unknown format — return with + prefix as best effort
	if !strings.HasPrefix(cleaned, "+") {
		return "+" + cleaned
	}
	return cleaned
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
