package handler

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/utils"
)

type UploadHandler struct{}

func NewUploadHandler() *UploadHandler {
	return &UploadHandler{}
}

func (h *UploadHandler) UploadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse multipart form
	err := r.ParseMultipartForm(10 << 20) // 10 MB max
	if err != nil {
		respondError(w, "File too large", http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondError(w, "No file uploaded", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate file type
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".xlsx" && ext != ".xls" && ext != ".csv" {
		respondError(w, "Invalid file type. Only Excel (.xlsx, .xls) and CSV files are allowed", http.StatusBadRequest)
		return
	}

	// Create uploads directory
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./uploads"
	}
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		respondError(w, "Failed to create upload directory", http.StatusInternalServerError)
		return
	}

	// Save file temporarily
	tempFile := filepath.Join(uploadDir, fmt.Sprintf("%d-%s", os.Getpid(), header.Filename))
	dst, err := os.Create(tempFile)
	if err != nil {
		respondError(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer dst.Close()
	defer os.Remove(tempFile) // Clean up after processing

	_, err = io.Copy(dst, file)
	if err != nil {
		respondError(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Parse file
	var contacts []types.Contact
	if ext == ".csv" {
		contacts, err = utils.ParseCSV(tempFile)
	} else {
		contacts, err = utils.ParseExcel(tempFile)
	}

	if err != nil {
		respondError(w, fmt.Sprintf("Failed to parse file: %v", err), http.StatusBadRequest)
		return
	}

	// Validate contacts
	validContacts := make([]types.Contact, 0)
	for _, contact := range contacts {
		if utils.IsValidPhone(contact.Phone) {
			validContacts = append(validContacts, contact)
		}
	}

	// Prepare response
	response := types.APIResponse{
		Success: true,
		Data: types.UploadResponse{
			Contacts:        validContacts,
			TotalContacts:   len(contacts),
			ValidContacts:   len(validContacts),
			InvalidContacts: len(contacts) - len(validContacts),
		},
	}

	respondJSON(w, response)
}
