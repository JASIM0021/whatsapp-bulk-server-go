package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	appdb "github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type apiKeyDoc struct {
	ID         primitive.ObjectID  `bson:"_id,omitempty"`
	UserID     primitive.ObjectID  `bson:"user_id"`
	Key        string              `bson:"key"`
	Name       string              `bson:"name"`
	IsActive   bool                `bson:"is_active"`
	LastUsedAt *time.Time          `bson:"last_used_at,omitempty"`
	CreatedAt  time.Time           `bson:"created_at"`
}

// APIKeyService manages developer API keys.
type APIKeyService struct {
	db  *appdb.DB
	sub *SubscriptionService
}

func NewAPIKeyService(database *appdb.DB, sub *SubscriptionService) *APIKeyService {
	return &APIKeyService{db: database, sub: sub}
}

// GenerateAPIKey creates a new API key for the given user.
// Requires an active subscription (any plan including free).
func (s *APIKeyService) GenerateAPIKey(ctx context.Context, userID, name string) (*types.APIKey, error) {
	active, err := s.sub.IsSubscriptionActive(ctx, userID)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, errors.New("api_access_requires_active_subscription")
	}

	// Generate key: "bsk_" + 32 hex chars (16 random bytes)
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	key := "bsk_" + hex.EncodeToString(b)

	if name == "" {
		name = "Default"
	}

	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}

	doc := apiKeyDoc{
		UserID:    uid,
		Key:       key,
		Name:      name,
		IsActive:  true,
		CreatedAt: time.Now().UTC(),
	}

	res, err := s.db.APIKeys().InsertOne(ctx, doc)
	if err != nil {
		return nil, err
	}

	insertedID := res.InsertedID.(primitive.ObjectID).Hex()
	return &types.APIKey{
		ID:         insertedID,
		Name:       name,
		Key:        key, // shown once on creation
		KeyPreview: keyPreview(key),
		IsActive:   true,
		CreatedAt:  doc.CreatedAt.Format(time.RFC3339),
	}, nil
}

// ListAPIKeys returns all keys for the user (Key field empty, only KeyPreview shown).
func (s *APIKeyService) ListAPIKeys(ctx context.Context, userID string) ([]types.APIKey, error) {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}

	cursor, err := s.db.APIKeys().Find(ctx, bson.M{"user_id": uid})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []apiKeyDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	result := make([]types.APIKey, 0, len(docs))
	for _, d := range docs {
		k := types.APIKey{
			ID:         d.ID.Hex(),
			Name:       d.Name,
			KeyPreview: keyPreview(d.Key),
			IsActive:   d.IsActive,
			CreatedAt:  d.CreatedAt.Format(time.RFC3339),
		}
		if d.LastUsedAt != nil {
			k.LastUsedAt = d.LastUsedAt.Format(time.RFC3339)
		}
		result = append(result, k)
	}
	return result, nil
}

// RevokeAPIKey sets is_active=false for the given key ID (must belong to userID).
func (s *APIKeyService) RevokeAPIKey(ctx context.Context, keyID, userID string) error {
	kid, err := primitive.ObjectIDFromHex(keyID)
	if err != nil {
		return errors.New("invalid key id")
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return errors.New("invalid user id")
	}

	res, err := s.db.APIKeys().UpdateOne(ctx,
		bson.M{"_id": kid, "user_id": uid},
		bson.M{"$set": bson.M{"is_active": false}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return errors.New("key not found")
	}
	return nil
}

// ValidateAPIKey looks up the key, checks it's active, and returns the owning userID.
func (s *APIKeyService) ValidateAPIKey(ctx context.Context, key string) (string, error) {
	var doc apiKeyDoc
	err := s.db.APIKeys().FindOne(ctx, bson.M{"key": key}).Decode(&doc)
	if err != nil {
		return "", errors.New("invalid or inactive API key")
	}
	if !doc.IsActive {
		return "", errors.New("invalid or inactive API key")
	}
	return doc.UserID.Hex(), nil
}

// UpdateLastUsed atomically updates last_used_at (best-effort, runs in background).
func (s *APIKeyService) UpdateLastUsed(ctx context.Context, key string) {
	now := time.Now().UTC()
	s.db.APIKeys().UpdateOne(ctx,
		bson.M{"key": key},
		bson.M{"$set": bson.M{"last_used_at": now}},
	)
}

// keyPreview returns a safe display version of the key: "bsk_XXXX...YYYY"
func keyPreview(key string) string {
	// key is "bsk_" + 32 hex chars = 36 chars total
	if len(key) <= 12 {
		return key
	}
	// show prefix "bsk_" + first 4 random chars + "..." + last 4 chars
	return key[:8] + "..." + key[len(key)-4:]
}
