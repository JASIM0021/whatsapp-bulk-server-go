package utils

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"github.com/xuri/excelize/v2"
)

// ParseExcel parses an Excel file and returns contacts
func ParseExcel(filePath string) ([]types.Contact, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open Excel file: %w", err)
	}
	defer f.Close()

	// Get first sheet
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("no sheets found in Excel file")
	}

	rows, err := f.GetRows(sheets[0])
	if err != nil {
		return nil, fmt.Errorf("failed to read rows: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("empty Excel file")
	}

	// Detect column indices
	phoneCol, nameCol := detectColumns(rows[0])
	if phoneCol == -1 {
		return nil, fmt.Errorf("could not find phone column")
	}

	// Parse contacts
	contacts := make([]types.Contact, 0)
	for i, row := range rows {
		if i == 0 {
			continue // Skip header
		}

		if phoneCol >= len(row) {
			continue
		}

		phone := cleanPhone(row[phoneCol])
		if phone == "" {
			continue
		}

		name := ""
		if nameCol != -1 && nameCol < len(row) {
			name = strings.TrimSpace(row[nameCol])
		}
		if name == "" {
			name = phone // Use phone as name if no name column
		}

		contacts = append(contacts, types.Contact{
			Phone: phone,
			Name:  name,
		})
	}

	return contacts, nil
}

// ParseCSV parses a CSV file and returns contacts
func ParseCSV(filePath string) ([]types.Contact, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open CSV file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("failed to read CSV: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("empty CSV file")
	}

	// Detect column indices
	phoneCol, nameCol := detectColumns(rows[0])
	if phoneCol == -1 {
		return nil, fmt.Errorf("could not find phone column")
	}

	// Parse contacts
	contacts := make([]types.Contact, 0)
	for i, row := range rows {
		if i == 0 {
			continue // Skip header
		}

		if phoneCol >= len(row) {
			continue
		}

		phone := cleanPhone(row[phoneCol])
		if phone == "" {
			continue
		}

		name := ""
		if nameCol != -1 && nameCol < len(row) {
			name = strings.TrimSpace(row[nameCol])
		}
		if name == "" {
			name = phone
		}

		contacts = append(contacts, types.Contact{
			Phone: phone,
			Name:  name,
		})
	}

	return contacts, nil
}

// detectColumns detects phone and name column indices from header row
func detectColumns(header []string) (phoneCol, nameCol int) {
	phoneCol = -1
	nameCol = -1

	phoneKeywords := []string{"phone", "mobile", "number", "contact", "whatsapp", "cell"}
	nameKeywords := []string{"name", "full name", "fullname", "customer", "client"}

	for i, col := range header {
		colLower := strings.ToLower(strings.TrimSpace(col))

		// Check for phone column
		for _, keyword := range phoneKeywords {
			if strings.Contains(colLower, keyword) {
				phoneCol = i
				break
			}
		}

		// Check for name column
		for _, keyword := range nameKeywords {
			if strings.Contains(colLower, keyword) {
				nameCol = i
				break
			}
		}
	}

	// If no phone column detected, assume first column
	if phoneCol == -1 && len(header) > 0 {
		phoneCol = 0
	}

	// If no name column and multiple columns, assume second column
	if nameCol == -1 && len(header) > 1 {
		nameCol = 1
	}

	return phoneCol, nameCol
}

// cleanPhone removes all non-digit characters from phone number
func cleanPhone(phone string) string {
	cleaned := ""
	for _, char := range phone {
		if char >= '0' && char <= '9' {
			cleaned += string(char)
		}
	}
	return cleaned
}
