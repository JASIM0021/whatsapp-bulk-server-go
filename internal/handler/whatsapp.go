package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type WhatsAppHandler struct {
	waService *service.WhatsAppService
}

func NewWhatsAppHandler(waService *service.WhatsAppService) *WhatsAppHandler {
	return &WhatsAppHandler{
		waService: waService,
	}
}

func (h *WhatsAppHandler) Initialize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("Received initialization request")

	err := h.waService.Initialize()
	if err != nil {
		logger.Error("Failed to initialize WhatsApp client: %v", err)
		respondError(w, fmt.Sprintf("Failed to initialize: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Success("WhatsApp client initialized successfully")
	respondJSON(w, types.APIResponse{
		Success: true,
		Message: "WhatsApp client initialized successfully",
	})
}

func (h *WhatsAppHandler) GetQRCode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Send initial connection
	fmt.Fprintf(w, ":\n\n")
	flusher.Flush()

	// Check current status and send immediately if already ready or has QR
	isConnected, isReady, hasQR := h.waService.GetStatus()

	if isReady && isConnected {
		logger.Info("Client already ready, sending ready event immediately")
		data, _ := json.Marshal(types.ProgressUpdate{
			Type: "ready",
			Data: "authenticated",
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// If there's a QR code already generated, send it immediately
	if hasQR {
		lastQR := h.waService.GetLastQR()
		if lastQR != "" {
			logger.Info("Sending cached QR code immediately")
			data, _ := json.Marshal(types.ProgressUpdate{
				Type: "qr",
				Data: lastQR,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	// Listen for events
	qrChan := h.waService.GetQRChannel()
	readyChan := h.waService.GetReadyChannel()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	timeout := time.After(2 * time.Minute)

	for {
		select {
		case qr := <-qrChan:
			logger.Info("Received new QR code, sending to client")
			data, _ := json.Marshal(types.ProgressUpdate{
				Type: "qr",
				Data: qr,
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-readyChan:
			logger.Info("Client ready, sending ready event")
			data, _ := json.Marshal(types.ProgressUpdate{
				Type: "ready",
				Data: "authenticated",
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return

		case <-ticker.C:
			// Keep-alive ping
			fmt.Fprintf(w, ":\n\n")
			flusher.Flush()

		case <-timeout:
			logger.Warn("QR code request timed out after 2 minutes")
			data, _ := json.Marshal(types.ProgressUpdate{
				Type: "error",
				Data: "QR code timeout",
			})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return

		case <-r.Context().Done():
			logger.Info("Client disconnected from QR stream")
			return
		}
	}
}

func (h *WhatsAppHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isConnected, isReady, hasQR := h.waService.GetStatus()

	respondJSON(w, types.APIResponse{
		Success: true,
		Data: types.WhatsAppStatus{
			IsConnected:   isConnected,
			IsReady:       isReady,
			HasQR:         hasQR,
			IsInitialized: isConnected || hasQR,
		},
	})
}

func (h *WhatsAppHandler) Disconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	logger.Info("Received disconnect request")
	h.waService.Disconnect()
	logger.Success("WhatsApp disconnected successfully")

	respondJSON(w, types.APIResponse{
		Success: true,
		Message: "WhatsApp disconnected successfully",
	})
}

func (h *WhatsAppHandler) SendMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req types.SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("Invalid request body: %v", err)
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Contacts) == 0 {
		logger.Error("No contacts provided in request")
		respondError(w, "No contacts provided", http.StatusBadRequest)
		return
	}

	logger.Section("NEW SEND REQUEST")
	logger.Info("📬 Total contacts: %d", len(req.Contacts))
	msgPreview := req.Message.Text
	if len(msgPreview) > 80 {
		msgPreview = msgPreview[:80] + "..."
	}
	logger.Info("📝 Message template: \"%s\"", msgPreview)
	logger.Info("👥 Contact list:")
	for i, c := range req.Contacts {
		logger.Info("   [%d] %s (%s)", i+1, c.Name, c.Phone)
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		logger.Error("HTTP streaming not supported - ResponseWriter doesn't implement http.Flusher")
		logger.Error("This usually means a middleware is wrapping the response writer incorrectly")
		respondError(w, "Streaming unsupported - SSE not available", http.StatusInternalServerError)
		return
	}

	logger.Debug("HTTP Flusher available - SSE streaming ready")

	// Check if client is ready
	if !h.waService.IsReady() {
		logger.Error("WhatsApp client not ready")
		data, _ := json.Marshal(types.ProgressUpdate{
			Type: "error",
			Data: "WhatsApp client not ready",
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	logger.Success("WhatsApp client is ready. Starting to send messages...")

	// Cleanup uploaded image after sending (if provided)
	if req.Message.ImagePath != "" {
		defer func() {
			logger.Info("Cleaning up uploaded image: %s", req.Message.ImagePath)
			if err := os.Remove(req.Message.ImagePath); err != nil {
				logger.Warn("Failed to delete uploaded image: %v", err)
			} else {
				logger.Success("Uploaded image deleted successfully")
			}
		}()
	}

	// Send messages
	progress := types.SendProgress{
		Total:  len(req.Contacts),
		Sent:   0,
		Failed: 0,
		Errors: make([]string, 0),
	}

	for i, contact := range req.Contacts {
		progress.Current = &req.Contacts[i]

		logger.Section(fmt.Sprintf("MESSAGE %d/%d", i+1, len(req.Contacts)))
		logger.WhatsAppSending(i+1, len(req.Contacts), contact.Name, contact.Phone)

		// Replace {{name}} placeholder
		message := strings.ReplaceAll(req.Message.Text, "{{name}}", contact.Name)
		if req.Message.Link != "" {
			message += "\n\n" + req.Message.Link
		}

		// Random delay between 3-5 seconds
		delay := time.Duration(3000+rand.Intn(2000)) * time.Millisecond

		// Only delay if this isn't the first message
		if i > 0 {
			logger.WhatsAppDelay(delay)
			time.Sleep(delay)
		}

		// Send message (with image if provided)
		var err error
		if req.Message.ImagePath != "" {
			// Send with uploaded image
			logger.Info("Sending message with uploaded image: %s", req.Message.ImagePath)
			err = h.waService.SendMessageWithImage(contact.Phone, message, req.Message.ImagePath)
		} else if req.Message.ImageURL != "" {
			// Send with image URL
			logger.Info("Sending message with image URL: %s", req.Message.ImageURL)
			err = h.waService.SendMessageWithImageURL(contact.Phone, message, req.Message.ImageURL)
		} else {
			// Send text-only message
			err = h.waService.SendMessage(contact.Phone, message)
		}
		if err != nil {
			progress.Failed++
			errorMsg := fmt.Sprintf("%s (%s): %v", contact.Name, contact.Phone, err)
			progress.Errors = append(progress.Errors, errorMsg)
			logger.WhatsAppFailed(i+1, len(req.Contacts), contact.Name, contact.Phone, err)
		} else {
			progress.Sent++
			logger.WhatsAppSuccess(i+1, len(req.Contacts), contact.Name, contact.Phone, "N/A")
		}

		// Send progress update
		data, _ := json.Marshal(types.ProgressUpdate{
			Type: "progress",
			Data: progress,
		})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Send completion
	logger.Section("SEND COMPLETE")
	logger.Success("✅ Sent: %d | ❌ Failed: %d | 📊 Total: %d", progress.Sent, progress.Failed, progress.Total)

	if len(progress.Errors) > 0 {
		logger.Warn("Errors encountered:")
		for i, err := range progress.Errors {
			logger.Error("  [%d] %s", i+1, err)
		}
	}

	progress.Current = nil
	data, _ := json.Marshal(types.ProgressUpdate{
		Type: "complete",
		Data: progress,
	})
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(types.APIResponse{
		Success: false,
		Error:   message,
	})
}
