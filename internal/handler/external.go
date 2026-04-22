package handler

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"strings"
	"time"

	appdb "github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// ExternalAPIHandler handles developer API endpoints (API-key-authenticated, JSON responses).
type ExternalAPIHandler struct {
	whatsappHandler  *WhatsAppHandler
	schedulerHandler *SchedulerHandler
	db               *appdb.DB
}

func NewExternalAPIHandler(wh *WhatsAppHandler, sh *SchedulerHandler, db *appdb.DB) *ExternalAPIHandler {
	return &ExternalAPIHandler{whatsappHandler: wh, schedulerHandler: sh, db: db}
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

	// ── Scheduled send ──────────────────────────────────────────────────────────
	if req.ScheduleAt != "" {
		scheduledAt, err := time.Parse(time.RFC3339, req.ScheduleAt)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid schedule_at format — use ISO 8601 (e.g. 2024-01-15T14:30:00Z)"})
			return
		}
		if scheduledAt.Before(time.Now().UTC().Add(30 * time.Second)) {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "schedule_at must be at least 30 seconds in the future"})
			return
		}

		// Build types.Contact slice
		typeContacts := make([]types.Contact, len(contacts))
		for i, c := range contacts {
			typeContacts[i] = types.Contact{Phone: c.Phone, Name: c.Name}
		}
		typeMessages := []types.Message{{Text: req.Message.Text, ImageURL: req.Message.ImageURL}}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		job, err := h.schedulerHandler.CreateJob(ctx, userID, typeContacts, typeMessages, scheduledAt, "")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "failed to schedule job"})
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"success":      true,
			"scheduled":    true,
			"job_id":       job.ID,
			"scheduled_at": scheduledAt.UTC().Format(time.RFC3339),
			"total":        len(contacts),
		})
		return
	}

	// ── Immediate send ──────────────────────────────────────────────────────────
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
