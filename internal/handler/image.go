package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type ImageHandler struct {
	uploadDir string
	maxSize   int64
}

func NewImageHandler() *ImageHandler {
	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = "./uploads"
	}

	// Create images subdirectory
	imagesDir := filepath.Join(uploadDir, "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		logger.Error("Failed to create images directory: %v", err)
	}

	maxSize := int64(10 * 1024 * 1024) // 10MB default
	return &ImageHandler{
		uploadDir: imagesDir,
		maxSize:   maxSize,
	}
}

func (h *ImageHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("Received image upload request")

	// Parse multipart form (max 10MB in memory)
	err := r.ParseMultipartForm(h.maxSize)
	if err != nil {
		logger.Error("Failed to parse multipart form: %v", err)
		respondError(w, "Failed to parse form data", http.StatusBadRequest)
		return
	}

	// Get the file from form
	file, header, err := r.FormFile("image")
	if err != nil {
		logger.Error("Failed to get image from form: %v", err)
		respondError(w, "No image file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	logger.Info("Received file: %s (size: %d bytes)", header.Filename, header.Size)

	// Validate file size
	if header.Size > h.maxSize {
		logger.Error("File too large: %d bytes (max: %d bytes)", header.Size, h.maxSize)
		respondError(w, fmt.Sprintf("File too large (max: %dMB)", h.maxSize/(1024*1024)), http.StatusBadRequest)
		return
	}

	// Validate file type
	contentType := header.Header.Get("Content-Type")
	if !isValidImageType(contentType) {
		logger.Error("Invalid file type: %s", contentType)
		respondError(w, "Invalid file type. Only images (jpg, jpeg, png, gif, webp) are allowed", http.StatusBadRequest)
		return
	}

	// Generate unique filename
	ext := filepath.Ext(header.Filename)
	if ext == "" {
		ext = getExtensionFromMimeType(contentType)
	}
	filename := fmt.Sprintf("img_%d%s", time.Now().UnixNano(), ext)
	filepath := filepath.Join(h.uploadDir, filename)

	// Create destination file
	dst, err := os.Create(filepath)
	if err != nil {
		logger.Error("Failed to create destination file: %v", err)
		respondError(w, "Failed to save image", http.StatusInternalServerError)
		return
	}

	// Copy uploaded file to destination
	written, err := io.Copy(dst, file)
	if err != nil {
		dst.Close()
		logger.Error("Failed to save image: %v", err)
		os.Remove(filepath) // Clean up on error
		respondError(w, "Failed to save image", http.StatusInternalServerError)
		return
	}

	// Explicitly close and flush to disk before returning the path
	if err := dst.Close(); err != nil {
		logger.Error("Failed to flush image file: %v", err)
		os.Remove(filepath)
		respondError(w, "Failed to save image", http.StatusInternalServerError)
		return
	}

	logger.Success("Image uploaded successfully: %s (%d bytes)", filename, written)

	// Return success response
	response := types.ImageUploadResponse{
		Success:  true,
		FilePath: filepath,
		FileName: filename,
		FileSize: written,
		MimeType: contentType,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func isValidImageType(contentType string) bool {
	validTypes := []string{
		"image/jpeg",
		"image/jpg",
		"image/png",
		"image/gif",
		"image/webp",
	}

	contentType = strings.ToLower(contentType)
	for _, validType := range validTypes {
		if contentType == validType {
			return true
		}
	}
	return false
}

func getExtensionFromMimeType(mimeType string) string {
	switch strings.ToLower(mimeType) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".jpg"
	}
}
