package main

import (
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

func parseAndNormalizePhone(phone string) (types.JID, string, error) {
	// Remove all non-digit characters except '+'
	cleanPhone := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleanPhone += string(char)
		}
	}

	if cleanPhone == "" {
		return types.JID{}, "", fmt.Errorf("invalid phone number: no digits found")
	}

	// Ensure phone starts with country code
	// If it doesn't start with a valid international prefix, it might need one
	if len(cleanPhone) < 10 {
		return types.JID{}, "", fmt.Errorf("phone number too short: %s", cleanPhone)
	}

	// Remove leading zeros (common issue)
	cleanPhone = strings.TrimLeft(cleanPhone, "0")

	// Create normalized phone with + prefix for WhatsApp check
	normalizedPhone := "+" + cleanPhone

	// Create JID (without + symbol)
	jid := types.JID{
		User:   cleanPhone,
		Server: types.DefaultUserServer,
	}

	fmt.Printf("Phone normalization:\n")
	fmt.Printf("  Input:      '%s'\n", phone)
	fmt.Printf("  Clean:      '%s'\n", cleanPhone)
	fmt.Printf("  Normalized: '%s'\n", normalizedPhone)
	fmt.Printf("  JID:        %s\n", jid.String())
	fmt.Printf("  JID User:   %s\n", jid.User)
	fmt.Printf("  JID Server: %s\n\n", jid.Server)

	return jid, normalizedPhone, nil
}

func main() {
	fmt.Println("=== Phone Number Formatting Test ===\n")

	// Test various phone number formats
	testNumbers := []string{
		"919876543210",      // India with country code
		"+919876543210",     // India with + and country code
		"9876543210",        // India without country code
		"1234567890",        // US format (10 digits)
		"+1234567890",       // US with +
		"(91) 98765-43210", // Formatted with special chars
		"91-9876543210",     // With dashes
		"0919876543210",     // With leading zero
	}

	for i, num := range testNumbers {
		fmt.Printf("Test #%d\n", i+1)
		fmt.Println("--------")
		_, _, err := parseAndNormalizePhone(num)
		if err != nil {
			fmt.Printf("ERROR: %v\n\n", err)
		}
	}

	fmt.Println("\n=== Important Notes ===")
	fmt.Println("1. Always include country code (e.g., 91 for India, 1 for US)")
	fmt.Println("2. Don't use leading zeros")
	fmt.Println("3. Remove all special characters (spaces, dashes, parentheses)")
	fmt.Println("4. Format: [country_code][phone_number]")
	fmt.Println("\nExample for India:")
	fmt.Println("  Correct: 919876543210")
	fmt.Println("  Wrong:   09876543210 (missing country code)")
	fmt.Println("  Wrong:   9876543210 (missing country code)")
}
