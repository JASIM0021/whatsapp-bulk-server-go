package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	googleoption "google.golang.org/api/option"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/logger"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// BotService manages bot configs and AI reply generation.
// Supports two AI providers controlled by the AI_PROVIDER env var:
//   - "openai"  → uses OPENAI_API_KEY  (default model: gpt-4o-mini)
//   - "gemini"  → uses GEMINI_API_KEY  (default model: gemini-1.5-flash)
//
// If AI_PROVIDER is unset it falls back to whichever key is present.
type BotService struct {
	db         *db.DB
	subService *SubscriptionService
	provider   string // "openai" | "gemini"
	openaiKey  string
	geminiKey  string
}

// NewBotService creates a new BotService, reading provider preference from env.
func NewBotService(database *db.DB, subService *SubscriptionService) *BotService {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("AI_PROVIDER")))
	openaiKey := os.Getenv("OPENAI_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")

	// Auto-detect if not explicitly set
	if provider == "" {
		if openaiKey != "" {
			provider = "openai"
		} else {
			provider = "gemini"
		}
	}

	return &BotService{
		db:         database,
		subService: subService,
		provider:   provider,
		openaiKey:  openaiKey,
		geminiKey:  geminiKey,
	}
}

// ── MongoDB document types ────────────────────────────────────────────────────

type botConfigDoc struct {
	ID              primitive.ObjectID `bson:"_id,omitempty"`
	UserID          primitive.ObjectID `bson:"user_id"`
	BusinessName    string             `bson:"business_name"`
	Description     string             `bson:"description"`
	Website         string             `bson:"website,omitempty"`
	Services        []string           `bson:"services"`
	BookingLink     string             `bson:"booking_link,omitempty"`
	ProductLink     string             `bson:"product_link,omitempty"`
	IsEnabled       bool               `bson:"is_enabled"`
	ExcludedNumbers []string           `bson:"excluded_numbers"`
	CreatedAt       time.Time          `bson:"created_at"`
	UpdatedAt       time.Time          `bson:"updated_at"`
}

type chatHistoryDoc struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	UserID       primitive.ObjectID `bson:"user_id"`
	ContactPhone string             `bson:"contact_phone"`
	Messages     []chatMessageDoc   `bson:"messages"`
	UpdatedAt    time.Time          `bson:"updated_at"`
}

type chatMessageDoc struct {
	Role    string `bson:"role"` // "user" | "model"
	Content string `bson:"content"`
}

// ── Public API ────────────────────────────────────────────────────────────────

// GetBotConfig returns the bot config for a user, or nil if not yet configured.
func (s *BotService) GetBotConfig(ctx context.Context, userID string) (*types.BotConfig, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}
	var doc botConfigDoc
	err = s.db.BotConfigs().FindOne(ctx, bson.M{"user_id": oid}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toBotConfigType(&doc), nil
}

// UpsertBotConfig creates or updates the bot config for a user.
func (s *BotService) UpsertBotConfig(ctx context.Context, userID string, req types.UpsertBotConfigRequest) (*types.BotConfig, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}
	now := time.Now()
	filter := bson.M{"user_id": oid}
	update := bson.M{
		"$set": bson.M{
			"business_name":    req.BusinessName,
			"description":      req.Description,
			"website":          req.Website,
			"services":         req.Services,
			"booking_link":     req.BookingLink,
			"product_link":     req.ProductLink,
			"is_enabled":       req.IsEnabled,
			"excluded_numbers": req.ExcludedNumbers,
			"updated_at":       now,
		},
		"$setOnInsert": bson.M{
			"user_id":    oid,
			"created_at": now,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc botConfigDoc
	if err := s.db.BotConfigs().FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		return nil, err
	}
	return toBotConfigType(&doc), nil
}

// HandleIncomingMessage checks if the user has an enabled bot, then generates
// and sends an AI reply. Runs inside a goroutine — must not block.
func (s *BotService) HandleIncomingMessage(
	ctx context.Context,
	userID string,
	senderPhone string,
	messageText string,
	waReply func(phone, text string) error,
) {
	logger.Info("Bot: processing message for user=%s from=%s provider=%s", userID, senderPhone, s.provider)

	// Check provider API key is configured
	if s.provider == "openai" && s.openaiKey == "" {
		logger.Warn("Bot: AI_PROVIDER=openai but OPENAI_API_KEY is not set — set it in .env")
		return
	}
	if s.provider == "gemini" && s.geminiKey == "" {
		logger.Warn("Bot: AI_PROVIDER=gemini but GEMINI_API_KEY is not set — set it in .env")
		return
	}

	// Load bot configuration
	cfg, err := s.GetBotConfig(ctx, userID)
	if err != nil {
		logger.Warn("Bot: failed to load config for user %s: %v", userID, err)
		return
	}
	if cfg == nil {
		logger.Info("Bot: no bot config found for user %s — go to /bot to create one", userID)
		return
	}
	if !cfg.IsEnabled {
		logger.Info("Bot: bot is disabled for user %s — enable it at /bot", userID)
		return
	}

	// Check if sender is in the exclusion list
	for _, excluded := range cfg.ExcludedNumbers {
		if strings.TrimSpace(excluded) == strings.TrimSpace(senderPhone) {
			logger.Info("Bot: skipping auto-reply for excluded number %s (user %s)", senderPhone, userID)
			return
		}
	}

	// Check subscription — any active plan is allowed (free plan respects message quota)
	sub, err := s.subService.GetSubscription(ctx, userID)
	if err != nil {
		logger.Warn("Bot: failed to get subscription for user %s: %v", userID, err)
		return
	}
	if sub == nil || !sub.IsActive {
		logger.Warn("Bot: user %s has no active subscription", userID)
		return
	}

	// For free-plan users, enforce the message quota before generating a reply
	if sub.Plan == types.PlanFree {
		remaining, err := s.subService.CheckMessageQuota(ctx, userID)
		if err != nil {
			logger.Warn("Bot: failed to check quota for user %s: %v", userID, err)
			return
		}
		if remaining == 0 {
			logger.Info("Bot: free-plan quota exhausted for user %s — skipping reply", userID)
			return
		}
	}

	logger.Info("Bot: generating reply for user=%s from=%s via %s", userID, senderPhone, s.provider)
	reply, err := s.generateReply(ctx, userID, senderPhone, messageText, cfg)
	if err != nil {
		logger.Warn("Bot: AI error for user %s (provider=%s): %v", userID, s.provider, err)
		return
	}

	logger.Info("Bot: sending reply to %s: %q", senderPhone, reply)
	if err := waReply(senderPhone, reply); err != nil {
		logger.Warn("Bot: failed to send bot reply to %s: %v", senderPhone, err)
		return
	}

	// Count bot replies against the free-plan quota (paid plans return early inside IncrementMessageCount)
	if sub.Plan == types.PlanFree {
		if _, _, err := s.subService.IncrementMessageCount(ctx, userID, 1); err != nil {
			logger.Warn("Bot: failed to increment message count for user %s: %v", userID, err)
		}
	}
}

// ── Reply generation ──────────────────────────────────────────────────────────

func (s *BotService) generateReply(
	ctx context.Context,
	userID string,
	contactPhone string,
	userMsg string,
	cfg *types.BotConfig,
) (string, error) {
	history, err := s.getChatHistory(ctx, userID, contactPhone)
	if err != nil {
		history = []chatMessageDoc{}
	}

	systemPrompt := buildSystemPrompt(cfg)

	var reply string
	switch s.provider {
	case "openai":
		reply, err = s.generateWithOpenAI(ctx, systemPrompt, history, userMsg)
	default: // "gemini"
		reply, err = s.generateWithGemini(ctx, systemPrompt, history, userMsg)
	}
	if err != nil {
		return "", err
	}

	// Persist history (keep last 20 turns)
	newHistory := append(history,
		chatMessageDoc{Role: "user", Content: userMsg},
		chatMessageDoc{Role: "model", Content: reply},
	)
	const maxHistory = 20
	if len(newHistory) > maxHistory {
		newHistory = newHistory[len(newHistory)-maxHistory:]
	}
	_ = s.saveChatHistory(ctx, userID, contactPhone, newHistory)

	return reply, nil
}

// generateWithGemini calls the Gemini 1.5 Flash model.
func (s *BotService) generateWithGemini(
	ctx context.Context,
	systemPrompt string,
	history []chatMessageDoc,
	userMsg string,
) (string, error) {
	client, err := genai.NewClient(ctx, googleoption.WithAPIKey(s.geminiKey))
	if err != nil {
		return "", fmt.Errorf("gemini client: %w", err)
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-1.5-flash")
	model.SystemInstruction = &genai.Content{
		Parts: []genai.Part{genai.Text(systemPrompt)},
	}

	// Convert stored history to Gemini format
	geminiHistory := make([]*genai.Content, 0, len(history))
	for _, h := range history {
		role := h.Role
		if role != "model" {
			role = "user"
		}
		geminiHistory = append(geminiHistory, &genai.Content{
			Role:  role,
			Parts: []genai.Part{genai.Text(h.Content)},
		})
	}

	chat := model.StartChat()
	chat.History = geminiHistory

	resp, err := chat.SendMessage(ctx, genai.Text(userMsg))
	if err != nil {
		return "", fmt.Errorf("gemini send: %w", err)
	}

	var sb strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if t, ok := part.(genai.Text); ok {
					sb.WriteString(string(t))
				}
			}
		}
	}
	reply := strings.TrimSpace(sb.String())
	if reply == "" {
		return "", fmt.Errorf("empty reply from Gemini")
	}
	return reply, nil
}

// generateWithOpenAI calls the GPT-4o-mini model.
func (s *BotService) generateWithOpenAI(
	ctx context.Context,
	systemPrompt string,
	history []chatMessageDoc,
	userMsg string,
) (string, error) {
	client := openai.NewClient(option.WithAPIKey(s.openaiKey))

	// Build messages array: system + history + current user message
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
	}
	for _, h := range history {
		if h.Role == "model" {
			messages = append(messages, openai.AssistantMessage(h.Content))
		} else {
			messages = append(messages, openai.UserMessage(h.Content))
		}
	}
	messages = append(messages, openai.UserMessage(userMsg))

	resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModelGPT4oMini,
		Messages: messages,
	})
	if err != nil {
		return "", fmt.Errorf("openai send: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from OpenAI")
	}
	reply := strings.TrimSpace(resp.Choices[0].Message.Content)
	if reply == "" {
		return "", fmt.Errorf("empty reply from OpenAI")
	}
	return reply, nil
}

// ── System prompt builder ─────────────────────────────────────────────────────

func buildSystemPrompt(cfg *types.BotConfig) string {
	var sb strings.Builder
	sb.WriteString("You are a helpful WhatsApp customer support assistant for ")
	sb.WriteString(cfg.BusinessName)
	sb.WriteString(".\n\n")
	sb.WriteString("Business Description: ")
	sb.WriteString(cfg.Description)
	sb.WriteString("\n\n")

	if len(cfg.Services) > 0 {
		sb.WriteString("Our key services/products:\n")
		for _, svc := range cfg.Services {
			sb.WriteString("- ")
			sb.WriteString(svc)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}
	if cfg.Website != "" {
		sb.WriteString("Website: ")
		sb.WriteString(cfg.Website)
		sb.WriteString("\n")
	}
	if cfg.BookingLink != "" {
		sb.WriteString("Booking link: ")
		sb.WriteString(cfg.BookingLink)
		sb.WriteString("\n")
	}
	if cfg.ProductLink != "" {
		sb.WriteString("Products link: ")
		sb.WriteString(cfg.ProductLink)
		sb.WriteString("\n")
	}
	sb.WriteString("\nInstructions:\n")
	sb.WriteString("- Be friendly, concise, and professional.\n")
	sb.WriteString("- Answer questions about our business, services, and products.\n")
	sb.WriteString("- If asked about booking or purchasing, share the relevant link.\n")
	sb.WriteString("- If you don't know something, politely say you'll pass the query to a human agent.\n")
	sb.WriteString("- Keep replies short — suitable for WhatsApp messages.\n")
	sb.WriteString("- Do not make up information not provided above.\n")
	return sb.String()
}

// ── Chat history persistence ──────────────────────────────────────────────────

func (s *BotService) getChatHistory(ctx context.Context, userID, contactPhone string) ([]chatMessageDoc, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	var doc chatHistoryDoc
	err = s.db.ChatHistory().FindOne(ctx, bson.M{
		"user_id":       oid,
		"contact_phone": contactPhone,
	}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return []chatMessageDoc{}, nil
	}
	if err != nil {
		return nil, err
	}
	return doc.Messages, nil
}

func (s *BotService) saveChatHistory(ctx context.Context, userID, contactPhone string, messages []chatMessageDoc) error {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}
	filter := bson.M{"user_id": oid, "contact_phone": contactPhone}
	update := bson.M{
		"$set": bson.M{
			"messages":   messages,
			"updated_at": time.Now(),
		},
		"$setOnInsert": bson.M{
			"user_id":       oid,
			"contact_phone": contactPhone,
		},
	}
	_, err = s.db.ChatHistory().UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// ── Helper ────────────────────────────────────────────────────────────────────

func toBotConfigType(doc *botConfigDoc) *types.BotConfig {
	excluded := doc.ExcludedNumbers
	if excluded == nil {
		excluded = []string{}
	}
	return &types.BotConfig{
		ID:              doc.ID.Hex(),
		UserID:          doc.UserID.Hex(),
		BusinessName:    doc.BusinessName,
		Description:     doc.Description,
		Website:         doc.Website,
		Services:        doc.Services,
		BookingLink:     doc.BookingLink,
		ProductLink:     doc.ProductLink,
		IsEnabled:       doc.IsEnabled,
		ExcludedNumbers: excluded,
		CreatedAt:       doc.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       doc.UpdatedAt.Format(time.RFC3339),
	}
}
