package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
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
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SchedulerHandler manages scheduled message jobs.
type SchedulerHandler struct {
	db              *appdb.DB
	whatsappHandler *WhatsAppHandler
	subService      *service.SubscriptionService
	mu              sync.Mutex
	running         map[string]bool // jobID -> in-flight
}

func NewSchedulerHandler(db *appdb.DB, wh *WhatsAppHandler, sub *service.SubscriptionService) *SchedulerHandler {
	return &SchedulerHandler{
		db:              db,
		whatsappHandler: wh,
		subService:      sub,
		running:         make(map[string]bool),
	}
}

// Start launches the background polling loop. Call once from main().
func (h *SchedulerHandler) Start() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			h.processdue()
		}
	}()
	logger.Success("Scheduler started — polling every 30 seconds")
}

// processdue finds and executes all pending jobs whose scheduled_at has passed.
func (h *SchedulerHandler) processdue() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	now := time.Now().UTC()
	cursor, err := h.db.ScheduledJobs().Find(ctx,
		bson.M{"status": types.JobStatusPending, "scheduled_at": bson.M{"$lte": now}},
		options.Find().SetLimit(20),
	)
	if err != nil {
		logger.Error("Scheduler: failed to query pending jobs: %v", err)
		return
	}
	defer cursor.Close(ctx)

	var jobs []types.ScheduledJob
	if err := cursor.All(ctx, &jobs); err != nil {
		logger.Error("Scheduler: failed to decode jobs: %v", err)
		return
	}

	for _, job := range jobs {
		h.mu.Lock()
		if h.running[job.ID] {
			h.mu.Unlock()
			continue
		}
		h.running[job.ID] = true
		h.mu.Unlock()

		go h.runJob(job)
	}
}

// runJob atomically claims a job (pending→running) then executes it.
func (h *SchedulerHandler) runJob(job types.ScheduledJob) {
	defer func() {
		h.mu.Lock()
		delete(h.running, job.ID)
		h.mu.Unlock()
	}()

	ctx := context.Background()

	// Atomically mark as running (prevents duplicate execution)
	oid, err := primitive.ObjectIDFromHex(job.ID)
	if err != nil {
		logger.Error("Scheduler: invalid job ID %s: %v", job.ID, err)
		return
	}
	res, err := h.db.ScheduledJobs().UpdateOne(ctx,
		bson.M{"_id": oid, "status": types.JobStatusPending},
		bson.M{"$set": bson.M{"status": types.JobStatusRunning}},
	)
	if err != nil || res.MatchedCount == 0 {
		// Another process already claimed it
		return
	}

	logger.Info("Scheduler: executing job %s for user %s (%d contacts)", job.ID, job.UserID, len(job.Contacts))

	// Determine messages
	messages := job.Messages

	// Get WhatsApp service
	waService, found := h.whatsappHandler.GetServiceForUser(job.UserID)
	if !found || !waService.IsReady() {
		h.finishJob(ctx, oid, types.JobStatusFailed, nil, "WhatsApp not connected at execution time")
		return
	}

	sent, failed := 0, 0
	var errs []string

	for i, contact := range job.Contacts {
		if i > 0 {
			delay := time.Duration(3000+rand.Intn(2000)) * time.Millisecond
			time.Sleep(delay)
		}

		contactFailed := false
		for j, msg := range messages {
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
				errs = append(errs, fmt.Sprintf("%s (%s) msg%d: %v", contact.Name, contact.Phone, j+1, sendErr))
			}
		}
		if contactFailed {
			failed++
		} else {
			sent++
			if h.subService != nil {
				h.subService.IncrementMessageCount(ctx, job.UserID, 1)
			}
		}
	}

	result := &types.ScheduledJobResult{
		Sent:   sent,
		Failed: failed,
		Total:  len(job.Contacts),
		Errors: errs,
	}
	status := types.JobStatusDone
	if failed > 0 && sent == 0 {
		status = types.JobStatusFailed
	}
	h.finishJob(ctx, oid, status, result, "")
	logger.Success("Scheduler: job %s done — sent=%d failed=%d", job.ID, sent, failed)
}

func (h *SchedulerHandler) finishJob(ctx context.Context, oid primitive.ObjectID, status string, result *types.ScheduledJobResult, errMsg string) {
	now := time.Now().UTC()
	update := bson.M{
		"$set": bson.M{
			"status":       status,
			"completed_at": now,
			"error_msg":    errMsg,
			"result":       result,
		},
	}
	h.db.ScheduledJobs().UpdateOne(ctx, bson.M{"_id": oid}, update)
}

// CreateJob inserts a new scheduled job into the DB. Exposed for external API use.
func (h *SchedulerHandler) CreateJob(ctx context.Context, userID string, contacts []types.Contact, messages []types.Message, scheduledAt time.Time, label string) (*types.ScheduledJob, error) {
	id := primitive.NewObjectID()
	now := time.Now().UTC()
	job := bson.M{
		"_id":          id,
		"user_id":      userID,
		"contacts":     contacts,
		"messages":     messages,
		"scheduled_at": scheduledAt,
		"status":       types.JobStatusPending,
		"label":        label,
		"created_at":   now,
	}
	if _, err := h.db.ScheduledJobs().InsertOne(ctx, job); err != nil {
		return nil, err
	}
	return &types.ScheduledJob{
		ID:          id.Hex(),
		UserID:      userID,
		Contacts:    contacts,
		Messages:    messages,
		ScheduledAt: scheduledAt,
		Status:      types.JobStatusPending,
		Label:       label,
		CreatedAt:   now,
	}, nil
}

// HandleCollection routes GET/POST on /api/schedule
func (h *SchedulerHandler) HandleCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listJobs(w, r)
	case http.MethodPost:
		h.createJob(w, r)
	default:
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// HandleSingle routes DELETE on /api/schedule/{id}
func (h *SchedulerHandler) HandleSingle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		respondError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.cancelJob(w, r)
}

func (h *SchedulerHandler) listJobs(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cursor, err := h.db.ScheduledJobs().Find(ctx,
		bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "scheduled_at", Value: -1}}).SetLimit(100),
	)
	if err != nil {
		respondError(w, "Failed to fetch scheduled jobs", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var jobs []types.ScheduledJob
	if err := cursor.All(ctx, &jobs); err != nil {
		respondError(w, "Failed to decode jobs", http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []types.ScheduledJob{}
	}
	respondJSON(w, types.APIResponse{Success: true, Data: jobs})
}

func (h *SchedulerHandler) createJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req types.ScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.Contacts) == 0 {
		respondError(w, "No contacts provided", http.StatusBadRequest)
		return
	}
	validMsgs := make([]types.Message, 0)
	for _, m := range req.Messages {
		if strings.TrimSpace(m.Text) != "" {
			validMsgs = append(validMsgs, m)
		}
	}
	if len(validMsgs) == 0 {
		respondError(w, "At least one message with text is required", http.StatusBadRequest)
		return
	}
	if req.ScheduledAt == "" {
		respondError(w, "scheduledAt is required", http.StatusBadRequest)
		return
	}

	scheduledAt, err := time.Parse(time.RFC3339, req.ScheduledAt)
	if err != nil {
		// try without timezone
		scheduledAt, err = time.Parse("2006-01-02T15:04", req.ScheduledAt)
		if err != nil {
			respondError(w, "Invalid scheduledAt format — use ISO 8601 (e.g. 2024-01-15T14:30:00Z)", http.StatusBadRequest)
			return
		}
		scheduledAt = scheduledAt.UTC()
	}
	if scheduledAt.Before(time.Now().UTC().Add(30 * time.Second)) {
		respondError(w, "scheduledAt must be at least 30 seconds in the future", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	job, err := h.CreateJob(ctx, userID, req.Contacts, validMsgs, scheduledAt, req.Label)
	if err != nil {
		respondError(w, "Failed to create scheduled job", http.StatusInternalServerError)
		return
	}

	logger.Info("Scheduled job %s for user %s at %s", job.ID, userID, scheduledAt.Format(time.RFC3339))
	respondJSON(w, types.APIResponse{Success: true, Data: job})
}

func (h *SchedulerHandler) cancelJob(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r)
	if !ok {
		respondError(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/schedule/")
	jobID := strings.TrimSpace(path)
	if jobID == "" {
		respondError(w, "Job ID required", http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(jobID)
	if err != nil {
		respondError(w, "Invalid job ID", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	res, err := h.db.ScheduledJobs().UpdateOne(ctx,
		bson.M{"_id": oid, "user_id": userID, "status": types.JobStatusPending},
		bson.M{"$set": bson.M{"status": types.JobStatusCancelled}},
	)
	if err != nil {
		respondError(w, "Failed to cancel job", http.StatusInternalServerError)
		return
	}
	if res.MatchedCount == 0 {
		respondError(w, "Job not found or cannot be cancelled (only pending jobs can be cancelled)", http.StatusNotFound)
		return
	}
	respondJSON(w, types.APIResponse{Success: true, Message: "Job cancelled"})
}

// HandleExternalList handles GET /api/v1/schedules (API-key auth)
func (h *SchedulerHandler) HandleExternalList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	userID, ok := middleware.GetUserID(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	cursor, err := h.db.ScheduledJobs().Find(ctx,
		bson.M{"user_id": userID},
		options.Find().SetSort(bson.D{{Key: "scheduled_at", Value: -1}}).SetLimit(50),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "failed to fetch schedules"})
		return
	}
	defer cursor.Close(ctx)

	var jobs []types.ScheduledJob
	if err := cursor.All(ctx, &jobs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"success": false, "error": "failed to decode schedules"})
		return
	}
	if jobs == nil {
		jobs = []types.ScheduledJob{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "schedules": jobs})
}

// HandleExternalCancel handles DELETE /api/v1/schedules/{id} (API-key auth)
func (h *SchedulerHandler) HandleExternalCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	userID, ok := middleware.GetUserID(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"success": false, "error": "unauthorized"})
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/schedules/")
	jobID := strings.TrimSpace(path)
	oid, err := primitive.ObjectIDFromHex(jobID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"success": false, "error": "invalid job ID"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	res, err := h.db.ScheduledJobs().UpdateOne(ctx,
		bson.M{"_id": oid, "user_id": userID, "status": types.JobStatusPending},
		bson.M{"$set": bson.M{"status": types.JobStatusCancelled}},
	)
	if err != nil || res.MatchedCount == 0 {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "job not found or not cancellable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "job cancelled"})
}
