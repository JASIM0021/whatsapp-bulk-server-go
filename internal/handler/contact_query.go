package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
)

// ContactQueryHandler handles the public contact-us form submission.
type ContactQueryHandler struct {
	db           *mongo.Database
	emailService *service.EmailService
}

// NewContactQueryHandler creates a new ContactQueryHandler.
func NewContactQueryHandler(db *mongo.Database, emailService *service.EmailService) *ContactQueryHandler {
	return &ContactQueryHandler{db: db, emailService: emailService}
}

type contactQueryRequest struct {
	Email string `json:"email"`
	Phone string `json:"phone"`
	Query string `json:"query"`
}

// Submit handles POST /api/contact — public, no auth required.
func (h *ContactQueryHandler) Submit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req contactQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	req.Query = strings.TrimSpace(req.Query)

	if req.Email == "" || req.Query == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "email and query are required"})
		return
	}

	// Fetch all admin emails from DB
	ctx := r.Context()
	cursor, err := h.db.Collection("users").Find(ctx, bson.M{"role": "admin"})
	var adminEmails []string
	if err == nil {
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var doc struct {
				Email string `bson:"email"`
			}
			if cursor.Decode(&doc) == nil && doc.Email != "" {
				adminEmails = append(adminEmails, doc.Email)
			}
		}
	}

	// Send notification to all admins
	for _, adminEmail := range adminEmails {
		_ = h.emailService.SendContactQueryAdminNotification(adminEmail, req.Email, req.Phone, req.Query)
	}

	// Send confirmation to the submitter
	_ = h.emailService.SendContactQueryConfirmation(req.Email, req.Query)

	// Store query in DB for reference
	h.db.Collection("contact_queries").InsertOne(ctx, bson.M{
		"email":      req.Email,
		"phone":      req.Phone,
		"query":      req.Query,
		"created_at": time.Now(),
		"status":     "open",
	})

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"success":true,"message":"Your query has been submitted. We'll get back to you shortly."}`)
}
