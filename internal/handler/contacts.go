package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	appdb "github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/middleware"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ContactsHandler handles the user contacts-book endpoints.
type ContactsHandler struct {
	db *appdb.DB
}

func NewContactsHandler(database *appdb.DB) *ContactsHandler {
	return &ContactsHandler{db: database}
}

// HandleCollection routes GET and POST on /api/contacts
func (h *ContactsHandler) HandleCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listContacts(w, r)
	case http.MethodPost:
		h.saveContacts(w, r)
	default:
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleSingle routes DELETE on /api/contacts/{id}
func (h *ContactsHandler) HandleSingle(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodDelete:
		h.deleteContact(w, r)
	default:
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listContacts GET /api/contacts — returns all saved contacts for the logged-in user.
func (h *ContactsHandler) listContacts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cursor, err := h.db.Contacts().Find(ctx, bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		respondError(w, "Failed to fetch contacts", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var contacts []types.SavedContact
	if err := cursor.All(ctx, &contacts); err != nil {
		respondError(w, "Failed to decode contacts", http.StatusInternalServerError)
		return
	}
	if contacts == nil {
		contacts = []types.SavedContact{}
	}

	respondJSON(w, types.APIResponse{Success: true, Data: contacts})
}

// saveContacts POST /api/contacts — bulk-upserts contacts (deduped by phone per user).
// Existing numbers are silently skipped; new ones are inserted.
func (h *ContactsHandler) saveContacts(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req types.SaveContactsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.Contacts) == 0 {
		respondJSON(w, types.APIResponse{Success: true, Message: "No contacts to save"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	now := time.Now().UTC().Format(time.RFC3339)
	inserted := 0

	for _, c := range req.Contacts {
		phone := strings.TrimSpace(c.Phone)
		if phone == "" {
			continue
		}

		filter := bson.M{"user_id": userID, "phone": phone}
		update := bson.M{
			"$setOnInsert": bson.M{
				"_id":        primitive.NewObjectID(),
				"user_id":    userID,
				"name":       strings.TrimSpace(c.Name),
				"phone":      phone,
				"created_at": now,
			},
		}
		opts := options.Update().SetUpsert(true)
		res, err := h.db.Contacts().UpdateOne(ctx, filter, update, opts)
		if err != nil {
			if mongo.IsDuplicateKeyError(err) {
				continue // silently skip duplicates
			}
			// log but keep processing the rest
			continue
		}
		if res.UpsertedCount > 0 {
			inserted++
		}
	}

	respondJSON(w, types.APIResponse{
		Success: true,
		Message: "Contacts saved",
		Data:    map[string]int{"inserted": inserted, "total": len(req.Contacts)},
	})
}

// deleteContact DELETE /api/contacts/{id} — deletes one saved contact.
// DELETE /api/contacts (no id) — deletes ALL contacts for the user.
func (h *ContactsHandler) deleteContact(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Extract id from path: /api/contacts/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/contacts")
	path = strings.TrimPrefix(path, "/")
	contactID := strings.TrimSpace(path)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if contactID == "" {
		// Delete all contacts for this user
		_, err := h.db.Contacts().DeleteMany(ctx, bson.M{"user_id": userID})
		if err != nil {
			respondError(w, "Failed to delete contacts", http.StatusInternalServerError)
			return
		}
		respondJSON(w, types.APIResponse{Success: true, Message: "All contacts deleted"})
		return
	}

	// Delete single contact — verify it belongs to this user
	oid, err := primitive.ObjectIDFromHex(contactID)
	if err != nil {
		respondError(w, "Invalid contact ID", http.StatusBadRequest)
		return
	}

	res, err := h.db.Contacts().DeleteOne(ctx, bson.M{"_id": oid, "user_id": userID})
	if err != nil {
		respondError(w, "Failed to delete contact", http.StatusInternalServerError)
		return
	}
	if res.DeletedCount == 0 {
		respondError(w, "Contact not found", http.StatusNotFound)
		return
	}

	respondJSON(w, types.APIResponse{Success: true, Message: "Contact deleted"})
}
