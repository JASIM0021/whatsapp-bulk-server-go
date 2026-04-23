package service

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
)

// SecurityService manages per-user security settings (auto-logout idle timer).
type SecurityService struct {
	db *db.DB
}

// SecuritySettings is the public DTO returned to the client.
type SecuritySettings struct {
	AutoLogoutEnabled bool      `json:"autoLogoutEnabled"`
	LastActivityAt    time.Time `json:"lastActivityAt"`
}

func NewSecurityService(database *db.DB) *SecurityService {
	return &SecurityService{db: database}
}

// GetSettings fetches the security settings for a user.
func (s *SecurityService) GetSettings(ctx context.Context, userID string) (*SecuritySettings, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}

	var result struct {
		AutoLogoutEnabled bool      `bson:"auto_logout_enabled"`
		LastActivityAt    time.Time `bson:"last_activity_at"`
	}
	if err := s.db.Users().FindOne(ctx, bson.M{"_id": oid}).Decode(&result); err != nil {
		return nil, err
	}

	return &SecuritySettings{
		AutoLogoutEnabled: result.AutoLogoutEnabled,
		LastActivityAt:    result.LastActivityAt,
	}, nil
}

// UpdateAutoLogout enables or disables the auto-logout feature for a user.
// When enabling, last_activity_at is reset to now so the 10-minute window starts fresh.
func (s *SecurityService) UpdateAutoLogout(ctx context.Context, userID string, enabled bool) error {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID")
	}

	_, err = s.db.Users().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{
		"$set": bson.M{
			"auto_logout_enabled": enabled,
			"last_activity_at":    time.Now(), // reset idle timer on toggle
		},
	})
	return err
}

// RecordActivity updates last_activity_at to now for a user.
// Only meaningful when auto_logout_enabled=true, but safe to call always.
func (s *SecurityService) RecordActivity(ctx context.Context, userID string) error {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID")
	}

	_, err = s.db.Users().UpdateOne(ctx, bson.M{"_id": oid}, bson.M{
		"$set": bson.M{"last_activity_at": time.Now()},
	})
	return err
}

// FindIdleUsers returns the IDs of users who have auto_logout_enabled=true
// and whose last_activity_at is older than idleThreshold.
func (s *SecurityService) FindIdleUsers(ctx context.Context, idleThreshold time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-idleThreshold)

	cursor, err := s.db.Users().Find(ctx, bson.M{
		"auto_logout_enabled": true,
		// last_activity_at must be set (non-zero) and older than cutoff
		"last_activity_at": bson.M{
			"$gt": time.Time{}, // non-zero
			"$lt": cutoff,
		},
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(docs))
	for _, d := range docs {
		ids = append(ids, d.ID.Hex())
	}
	return ids, nil
}
