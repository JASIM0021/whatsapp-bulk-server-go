package utils

import (
	"regexp"
	"strings"
)

// IsValidPhone validates a phone number
func IsValidPhone(phone string) bool {
	// Remove all non-digit characters
	cleaned := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleaned += string(char)
		}
	}

	// Phone number should be between 10-15 digits
	if len(cleaned) < 10 || len(cleaned) > 15 {
		return false
	}

	// Should contain only digits
	match, _ := regexp.MatchString(`^\d+$`, cleaned)
	return match
}

// FormatPhone formats a phone number for WhatsApp
func FormatPhone(phone string) string {
	// Remove all non-digit characters
	cleaned := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleaned += string(char)
		}
	}

	// Remove leading zeros or plus
	cleaned = strings.TrimLeft(cleaned, "0")
	cleaned = strings.TrimLeft(cleaned, "+")

	return cleaned
}
