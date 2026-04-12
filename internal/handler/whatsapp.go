package handler

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	appdb "github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type WhatsAppHandler struct {
	mu         sync.RWMutex
	services   map[string]*service.WhatsAppService
	dbDir      string
	db         *appdb.DB
	subService *service.SubscriptionService
}

func NewWhatsAppHandler(dbDir string, database *appdb.DB) *WhatsAppHandler {
	absDir, _ := filepath.Abs(dbDir)
	os.MkdirAll(absDir, 0755)
	return &WhatsAppHandler{
		services: make(map[string]*service.WhatsAppService),
		dbDir:    absDir,
		db:       database,
	}
}

// SetSubscriptionService sets the subscription service for message quota tracking.
func (h *WhatsAppHandler) SetSubscriptionService(subSvc *service.SubscriptionService) {
	h.subService = subSvc
}

// Shutdown gracefully closes all active WhatsApp WebSocket connections.
// Sessions are preserved in SQLite so users reconnect automatically on restart.
func (h *WhatsAppHandler) Shutdown() {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, svc := range h.services {
		svc.GracefulShutdown()
	}
}

// GetServiceForUser returns the WhatsAppService for a given userID if it exists (read-only, no auto-init).
func (h *WhatsAppHandler) GetServiceForUser(userID string) (*service.WhatsAppService, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	svc, ok := h.services[userID]
	return svc, ok
}

func (h *WhatsAppHandler) getOrCreateService(userID string) (*service.WhatsAppService, error) {
	h.mu.RLock()
	svc, ok := h.services[userID]
	h.mu.RUnlock()
	if ok {
		return svc, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// Double-check after acquiring write lock
	if svc, ok := h.services[userID]; ok {
		return svc, nil
	}

	dbPath := fmt.Sprintf("%s/whatsapp_session_%s.db", h.dbDir, userID)
	svc, err := service.NewWhatsAppServiceWithPath(dbPath, userID, h.db)
	if err != nil {
		return nil, err
	}
	h.services[userID] = svc

	// If a session was restored from MongoDB, auto-reconnect in the background
	// so users don't need to manually re-init after a server restart / PM2 reload.
	if svc.HasRestoredSession() {
		go func() {
			if err := svc.Initialize(); err != nil {
				logger.Info("Auto-reconnect failed for user %s: %v (user may need to re-scan QR)", userID, err)
			}
		}()
	}

	return svc, nil
}

func (h *WhatsAppHandler) Initialize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	waService, err := h.getOrCreateService(userID)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to initialize: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Info("Received initialization request for user %s", userID)

	if err := waService.Initialize(); err != nil {
		logger.Error("Failed to initialize WhatsApp client: %v", err)
		respondError(w, fmt.Sprintf("Failed to initialize: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Success("WhatsApp client initialized for user %s", userID)
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

	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	waService, err := h.getOrCreateService(userID)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}

	// Auto-initialize if the client is not yet running
	isConnected, isReady, _ := waService.GetStatus()
	if !isConnected && !isReady {
		logger.Info("Auto-initializing WhatsApp client for QR stream (user %s)", userID)
		if err := waService.Initialize(); err != nil {
			respondError(w, fmt.Sprintf("Failed to initialize WhatsApp: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
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
	isConnected, isReady, hasQR := waService.GetStatus()

	if isReady && isConnected {
		logger.Info("Client already ready for user %s, sending ready event immediately", userID)
		data, _ := json.Marshal(types.ProgressUpdate{Type: "ready", Data: "authenticated"})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	if hasQR {
		if lastQR := waService.GetLastQR(); lastQR != "" {
			logger.Info("Sending cached QR code for user %s", userID)
			data, _ := json.Marshal(types.ProgressUpdate{Type: "qr", Data: lastQR})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}

	qrChan := waService.GetQRChannel()
	readyChan := waService.GetReadyChannel()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	timeout := time.After(2 * time.Minute)

	for {
		select {
		case qr := <-qrChan:
			logger.Info("Received new QR code for user %s", userID)
			data, _ := json.Marshal(types.ProgressUpdate{Type: "qr", Data: qr})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()

		case <-readyChan:
			logger.Info("Client ready for user %s", userID)
			data, _ := json.Marshal(types.ProgressUpdate{Type: "ready", Data: "authenticated"})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return

		case <-ticker.C:
			fmt.Fprintf(w, ":\n\n")
			flusher.Flush()

		case <-timeout:
			logger.Warn("QR code request timed out for user %s — reinitializing", userID)
			// Auto-reinitialize to generate a fresh QR code instead of giving up
			go func() {
				if err := waService.Reinitialize(); err != nil {
					logger.Error("Auto-reinitialize on timeout failed for user %s: %v", userID, err)
				}
			}()
			data, _ := json.Marshal(types.ProgressUpdate{Type: "timeout", Data: "QR code expired, reconnecting..."})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return

		case <-r.Context().Done():
			logger.Info("Client disconnected from QR stream (user %s)", userID)
			return
		}
	}
}

func (h *WhatsAppHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	waService, err := h.getOrCreateService(userID)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
		return
	}

	isConnected, isReady, hasQR := waService.GetStatus()
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

	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.mu.RLock()
	waService, exists := h.services[userID]
	h.mu.RUnlock()

	if exists {
		waService.Disconnect()
		h.mu.Lock()
		delete(h.services, userID)
		h.mu.Unlock()
		logger.Success("WhatsApp disconnected for user %s", userID)
	}

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

	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	waService, err := h.getOrCreateService(userID)
	if err != nil {
		respondError(w, fmt.Sprintf("Failed to get session: %v", err), http.StatusInternalServerError)
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
		respondError(w, "No contacts provided", http.StatusBadRequest)
		return
	}

	logger.Section("NEW SEND REQUEST")
	logger.Info("📬 User %s | Total contacts: %d", userID, len(req.Contacts))

	// Check message quota for free-plan users
	if h.subService != nil {
		remaining, err := h.subService.CheckMessageQuota(r.Context(), userID)
		if err != nil {
			logger.Error("Failed to check message quota for user %s: %v", userID, err)
			respondError(w, "Failed to check message quota", http.StatusInternalServerError)
			return
		}
		if remaining == 0 {
			respondError(w, "Free trial message limit reached. Please upgrade your plan.", http.StatusForbidden)
			return
		}
		// If remaining is positive (free plan), cap contacts to remaining quota
		if remaining > 0 && len(req.Contacts) > remaining {
			logger.Warn("User %s requested %d messages but only %d remaining, capping", userID, len(req.Contacts), remaining)
			req.Contacts = req.Contacts[:remaining]
		}
	}

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	if !waService.IsReady() {
		data, _ := json.Marshal(types.ProgressUpdate{Type: "error", Data: "WhatsApp client not ready"})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
		return
	}

	// Validate and cleanup uploaded image after sending
	if req.Message.ImagePath != "" {
		// Verify the image file exists before attempting to send
		info, err := os.Stat(req.Message.ImagePath)
		if err != nil {
			logger.Error("Image file not found at path '%s': %v", req.Message.ImagePath, err)
			data, _ := json.Marshal(types.ProgressUpdate{Type: "error", Data: fmt.Sprintf("Image file not found: %v", err)})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}
		logger.Info("Image file verified: %s (size: %d bytes)", req.Message.ImagePath, info.Size())

		defer func() {
			logger.Info("Cleaning up uploaded image: %s", req.Message.ImagePath)
			if err := os.Remove(req.Message.ImagePath); err != nil {
				logger.Warn("Failed to delete uploaded image: %v", err)
			}
		}()
	}

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

		// Replace {{name}} placeholder with contact name
		message := strings.ReplaceAll(req.Message.Text, "{{name}}", contact.Name)
		if req.Message.Link != "" {
			message += "\n\n" + req.Message.Link
		}

		// Random delay between 3-5 seconds (skip first message)
		if i > 0 {
			delay := time.Duration(3000+rand.Intn(2000)) * time.Millisecond
			logger.WhatsAppDelay(delay)
			time.Sleep(delay)
		}

		var sendErr error
		if req.Message.ImagePath != "" {
			sendErr = waService.SendMessageWithImage(contact.Phone, message, req.Message.ImagePath)
		} else if req.Message.ImageURL != "" {
			sendErr = waService.SendMessageWithImageURL(contact.Phone, message, req.Message.ImageURL)
		} else {
			sendErr = waService.SendMessage(contact.Phone, message)
		}

		if sendErr != nil {
			progress.Failed++
			progress.Errors = append(progress.Errors, fmt.Sprintf("%s (%s): %v", contact.Name, contact.Phone, sendErr))
			logger.WhatsAppFailed(i+1, len(req.Contacts), contact.Name, contact.Phone, sendErr)
		} else {
			progress.Sent++
			logger.WhatsAppSuccess(i+1, len(req.Contacts), contact.Name, contact.Phone, "N/A")
			// Increment message count for free-plan users
			if h.subService != nil {
				h.subService.IncrementMessageCount(r.Context(), userID, 1)
			}
		}

		data, _ := json.Marshal(types.ProgressUpdate{Type: "progress", Data: progress})
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	logger.Section("SEND COMPLETE")
	logger.Success("✅ Sent: %d | ❌ Failed: %d | Total: %d", progress.Sent, progress.Failed, progress.Total)

	progress.Current = nil
	data, _ := json.Marshal(types.ProgressUpdate{Type: "complete", Data: progress})
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
