package handler

import (
	"context"
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
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type WhatsAppHandler struct {
	mu              sync.RWMutex
	services        map[string]*service.WhatsAppService
	sharedContainer service.SharedContainer // one SQLite DB for all users
	dbDir           string
	db              *appdb.DB
	subService      *service.SubscriptionService
	botService      *service.BotService
	imageHandler    *ImageHandler
}

func NewWhatsAppHandler(dbDir string, database *appdb.DB) *WhatsAppHandler {
	absDir, _ := filepath.Abs(dbDir)
	os.MkdirAll(absDir, 0755)

	sharedDBPath := filepath.Join(absDir, "sessions.db")
	container, err := service.NewSharedContainer(sharedDBPath)
	if err != nil {
		logger.Error("Failed to open shared WhatsApp session DB at %s: %v", sharedDBPath, err)
		// container will be nil; services will fail to initialize and log clearly
	}

	return &WhatsAppHandler{
		services:        make(map[string]*service.WhatsAppService),
		dbDir:           absDir,
		db:              database,
		sharedContainer: container,
	}
}

// SetImageHandler wires the image handler so SendMessages can validate path ownership.
func (h *WhatsAppHandler) SetImageHandler(ih *ImageHandler) {
	h.imageHandler = ih
}

// SetSubscriptionService sets the subscription service for message quota tracking.
func (h *WhatsAppHandler) SetSubscriptionService(subSvc *service.SubscriptionService) {
	h.subService = subSvc
}

// SetBotService injects the bot service so new WhatsApp sessions get bot support.
func (h *WhatsAppHandler) SetBotService(botSvc *service.BotService) {
	h.mu.Lock()
	h.botService = botSvc
	// Inject into any already-running services
	for _, svc := range h.services {
		svc.SetBotService(botSvc)
	}
	h.mu.Unlock()
}

// Shutdown gracefully closes all active WhatsApp WebSocket connections then closes
// the shared SQLite container. Sessions are preserved in SQLite so users reconnect
// automatically on next startup without re-scanning QR.
func (h *WhatsAppHandler) Shutdown() {
	h.mu.RLock()
	for _, svc := range h.services {
		svc.GracefulShutdown()
	}
	h.mu.RUnlock()

	if h.sharedContainer != nil {
		_ = h.sharedContainer.Close()
	}
}

// GetServiceForUser returns the WhatsAppService for a given userID if it exists (read-only, no auto-init).
// The returned service is guaranteed to have svc.GetUserID() == userID.
func (h *WhatsAppHandler) GetServiceForUser(userID string) (*service.WhatsAppService, bool) {
	if userID == "" {
		return nil, false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	svc, ok := h.services[userID]
	if ok && svc.GetUserID() != userID {
		// Defensive: should never happen — map key == service.userID by construction.
		logger.Error("ISOLATION BUG: service in map[%s] has userID=%s — rejecting", userID, svc.GetUserID())
		return nil, false
	}
	return svc, ok
}

func (h *WhatsAppHandler) getOrCreateService(userID string) (*service.WhatsAppService, error) {
	if userID == "" {
		return nil, fmt.Errorf("userID must not be empty")
	}
	h.mu.RLock()
	svc, ok := h.services[userID]
	h.mu.RUnlock()
	if ok {
		// Ownership assertion — map key must equal embedded userID
		if svc.GetUserID() != userID {
			logger.Error("ISOLATION BUG: service in map[%s] has userID=%s", userID, svc.GetUserID())
			return nil, fmt.Errorf("session ownership mismatch — please contact support")
		}
		return svc, nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	// Double-check after acquiring write lock
	if svc, ok := h.services[userID]; ok {
		return svc, nil
	}

	svc, err := service.NewWhatsAppServiceWithContainer(h.sharedContainer, userID, h.db)
	if err != nil {
		return nil, err
	}
	// Final ownership check before storing
	if svc.GetUserID() != userID {
		return nil, fmt.Errorf("ISOLATION BUG: newly created service has wrong userID")
	}
	h.services[userID] = svc

	// Inject bot service if available
	if h.botService != nil {
		svc.SetBotService(h.botService)
	}

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

	// Determine effective messages: use Messages array if provided, else wrap single Message
	messages := req.Messages
	if len(messages) == 0 {
		messages = []types.Message{req.Message}
	}

	// Verify all uploaded image files exist, enforce ownership, and schedule cleanup
	var imagePaths []string
	for _, msg := range messages {
		if msg.ImagePath == "" {
			continue
		}
		// Reject any path that doesn't belong to this user (path traversal / cross-user access)
		if h.imageHandler != nil && !h.imageHandler.IsOwnedBy(msg.ImagePath, userID) {
			logger.Error("ISOLATION: user %s tried to send image owned by another user: %s", userID, msg.ImagePath)
			data, _ := json.Marshal(types.ProgressUpdate{Type: "error", Data: "Image access denied"})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}
		info, err := os.Stat(msg.ImagePath)
		if err != nil {
			logger.Error("Image file not found at path '%s': %v", msg.ImagePath, err)
			data, _ := json.Marshal(types.ProgressUpdate{Type: "error", Data: fmt.Sprintf("Image file not found: %v", err)})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			return
		}
		logger.Info("Image file verified: %s (size: %d bytes)", msg.ImagePath, info.Size())
		imagePaths = append(imagePaths, msg.ImagePath)
	}
	if len(imagePaths) > 0 {
		defer func() {
			for _, p := range imagePaths {
				logger.Info("Cleaning up uploaded image: %s", p)
				if err := os.Remove(p); err != nil {
					logger.Warn("Failed to delete uploaded image: %v", err)
				}
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

		logger.Section(fmt.Sprintf("CONTACT %d/%d", i+1, len(req.Contacts)))
		logger.WhatsAppSending(i+1, len(req.Contacts), contact.Name, contact.Phone)

		// Random delay between 3-5 seconds between contacts (skip first)
		if i > 0 {
			delay := time.Duration(3000+rand.Intn(2000)) * time.Millisecond
			logger.WhatsAppDelay(delay)
			time.Sleep(delay)
		}

		contactFailed := false
		for j, msg := range messages {
			// Short delay between multiple messages to the same contact
			if j > 0 {
				time.Sleep(1 * time.Second)
			}

			text := strings.ReplaceAll(msg.Text, "{{name}}", contact.Name)
			if msg.Link != "" {
				text += "\n\n" + msg.Link
			}

			var sendErr error
			if msg.ImagePath != "" {
				sendErr = waService.SendMessageWithImage(contact.Phone, text, msg.ImagePath)
			} else if msg.ImageURL != "" {
				sendErr = waService.SendMessageWithImageURL(contact.Phone, text, msg.ImageURL)
			} else {
				sendErr = waService.SendMessage(contact.Phone, text)
			}

			if sendErr != nil {
				contactFailed = true
				progress.Errors = append(progress.Errors, fmt.Sprintf("%s (%s) msg%d: %v", contact.Name, contact.Phone, j+1, sendErr))
				logger.WhatsAppFailed(i+1, len(req.Contacts), contact.Name, contact.Phone, sendErr)
			} else {
				logger.WhatsAppSuccess(i+1, len(req.Contacts), contact.Name, contact.Phone, "N/A")
			}
		}

		if contactFailed {
			progress.Failed++
		} else {
			progress.Sent++
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

// GetContacts returns all contacts from the user's active WhatsApp session store.
func (h *WhatsAppHandler) GetContacts(w http.ResponseWriter, r *http.Request) {
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

	contacts, err := waService.GetWhatsAppContacts(r.Context())
	if err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, types.APIResponse{
		Success: true,
		Data:    contacts,
	})
}

// DisconnectUser disconnects the WhatsApp session for a specific user and removes
// it from the in-memory map. Used by the auto-logout idle checker.
func (h *WhatsAppHandler) DisconnectUser(userID string) {
	h.mu.RLock()
	svc, ok := h.services[userID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	svc.Disconnect()
	h.mu.Lock()
	delete(h.services, userID)
	h.mu.Unlock()
	logger.Info("Auto-logout: WhatsApp session disconnected for user %s", userID)
}

// AutoStartBotSessions queries MongoDB for all users with an enabled bot config
// and auto-initializes their WhatsApp sessions so the bot starts listening
// immediately on server start — without requiring the user to open the web panel.
func (h *WhatsAppHandler) AutoStartBotSessions(ctx context.Context) {
	if h.db == nil {
		return
	}

	type botConfigMinimal struct {
		UserID    primitive.ObjectID `bson:"user_id"`
		IsEnabled bool               `bson:"is_enabled"`
	}

	cursor, err := h.db.BotConfigs().Find(ctx, bson.M{"is_enabled": true})
	if err != nil {
		logger.Warn("AutoStartBotSessions: failed to query bot_configs: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var configs []botConfigMinimal
	if err := cursor.All(ctx, &configs); err != nil {
		logger.Warn("AutoStartBotSessions: failed to decode bot_configs: %v", err)
		return
	}

	if len(configs) == 0 {
		logger.Info("AutoStartBotSessions: no active bot sessions to restore")
		return
	}

	logger.Info("AutoStartBotSessions: restoring %d bot session(s)...", len(configs))
	for _, cfg := range configs {
		userID := cfg.UserID.Hex()
		go func(uid string) {
			_, err := h.getOrCreateService(uid)
			if err != nil {
				logger.Warn("AutoStartBotSessions: failed to init session for user %s: %v", uid, err)
			} else {
				logger.Info("AutoStartBotSessions: session initialized for user %s", uid)
			}
		}(userID)
	}
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
