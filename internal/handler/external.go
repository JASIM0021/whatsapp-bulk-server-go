package handler

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// ExternalAPIHandler handles developer API endpoints (API-key-authenticated, JSON responses).
type ExternalAPIHandler struct {
	whatsappHandler *WhatsAppHandler
}

func NewExternalAPIHandler(wh *WhatsAppHandler) *ExternalAPIHandler {
	return &ExternalAPIHandler{whatsappHandler: wh}
}

// POST /api/v1/send
func (h *ExternalAPIHandler) Send(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	userID, ok := middleware.GetUserID(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	var req types.ExternalSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid request body"})
		return
	}

	// Build contacts list
	type contact struct {
		Phone string
		Name  string
	}
	var contacts []contact

	if req.Phone != "" {
		contacts = append(contacts, contact{Phone: req.Phone})
	} else {
		for _, c := range req.Contacts {
			contacts = append(contacts, contact{Phone: c.Phone, Name: c.Name})
		}
	}

	if len(contacts) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "no recipients specified"})
		return
	}
	if len(contacts) > 50 {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "maximum 50 contacts per call"})
		return
	}
	if strings.TrimSpace(req.Message.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "message.text is required"})
		return
	}

	// Get the user's WhatsApp service (read-only — do NOT auto-init)
	waService, found := h.whatsappHandler.GetServiceForUser(userID)
	if !found || !waService.IsReady() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"success": false,
			"error":   "WhatsApp not connected. Please connect via the dashboard first.",
		})
		return
	}

	sent, failed := 0, 0
	var errs []string

	for i, c := range contacts {
		if i > 0 {
			delay := time.Duration(3000+rand.Intn(2000)) * time.Millisecond
			time.Sleep(delay)
		}

		text := req.Message.Text
		if c.Name != "" {
			text = strings.ReplaceAll(text, "{{name}}", c.Name)
		}

		var sendErr error
		if req.Message.ImageURL != "" {
			sendErr = waService.SendMessageWithImageURL(c.Phone, text, req.Message.ImageURL)
		} else {
			sendErr = waService.SendMessage(c.Phone, text)
		}

		if sendErr != nil {
			failed++
			errs = append(errs, c.Phone+": "+sendErr.Error())
		} else {
			sent++
		}
	}

	resp := types.ExternalSendResponse{
		Success: failed == 0,
		Sent:    sent,
		Failed:  failed,
		Total:   len(contacts),
		Errors:  errs,
	}
	writeJSON(w, http.StatusOK, resp)
}
