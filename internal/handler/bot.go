package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// BotHandler handles bot configuration CRUD.
type BotHandler struct {
	botService *service.BotService
}

func NewBotHandler(botService *service.BotService) *BotHandler {
	return &BotHandler{botService: botService}
}

// HandleBot handles GET (fetch config) and POST/PUT (upsert config).
// GET  /api/bot
// POST /api/bot
func (h *BotHandler) HandleBot(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(types.UserIDKey).(string)
	if !ok || userID == "" {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, err := h.botService.GetBotConfig(r.Context(), userID)
		if err != nil {
			respondError(w, "Failed to fetch bot config", http.StatusInternalServerError)
			return
		}
		respondJSON(w, map[string]interface{}{
			"success": true,
			"data":    cfg,
		})

	case http.MethodPost, http.MethodPut:
		var req types.UpsertBotConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		// When a custom system prompt is provided, business info fields are optional.
		hasCustomPrompt := strings.TrimSpace(req.CustomSystemPrompt) != ""
		if !hasCustomPrompt && (strings.TrimSpace(req.BusinessName) == "" || strings.TrimSpace(req.Description) == "") {
			respondError(w, "businessName and description are required (or provide a custom system prompt)", http.StatusBadRequest)
			return
		}

		cfg, err := h.botService.UpsertBotConfig(r.Context(), userID, req)
		if err != nil {
			respondError(w, "Failed to save bot config", http.StatusInternalServerError)
			return
		}
		respondJSON(w, map[string]interface{}{
			"success": true,
			"data":    cfg,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
