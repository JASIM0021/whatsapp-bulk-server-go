package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type TemplateHandler struct {
	templateService *service.TemplateService
}

func NewTemplateHandler(templateService *service.TemplateService) *TemplateHandler {
	return &TemplateHandler{templateService: templateService}
}

// HandleCollection handles GET /api/templates and POST /api/templates
func (h *TemplateHandler) HandleCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.list(w, r)
	case http.MethodPost:
		h.create(w, r)
	default:
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Single handles GET/PUT/DELETE /api/templates/{id}
func (h *TemplateHandler) Single(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	templateID := strings.TrimPrefix(r.URL.Path, "/api/templates/")
	if templateID == "" {
		respondError(w, "Invalid template ID", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var req types.UpdateTemplateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		t, err := h.templateService.Update(r.Context(), userID, templateID, req)
		if err != nil {
			respondError(w, err.Error(), http.StatusBadRequest)
			return
		}
		respondJSON(w, types.APIResponse{Success: true, Data: t})

	case http.MethodDelete:
		if err := h.templateService.Delete(r.Context(), userID, templateID); err != nil {
			if err.Error() == "cannot delete default template" {
				respondError(w, err.Error(), http.StatusForbidden)
			} else {
				respondError(w, err.Error(), http.StatusBadRequest)
			}
			return
		}
		respondJSON(w, types.APIResponse{Success: true, Message: "Template deleted"})

	default:
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *TemplateHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	templates, err := h.templateService.List(r.Context(), userID)
	if err != nil {
		respondError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	respondJSON(w, types.APIResponse{Success: true, Data: templates})
}

func (h *TemplateHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var req types.CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	t, err := h.templateService.Create(r.Context(), userID, req)
	if err != nil {
		respondError(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(types.APIResponse{Success: true, Data: t})
}
