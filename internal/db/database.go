package db

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/bson"
)

type DB struct {
	client *mongo.Client
	mdb    *mongo.Database
}

func New(uri string) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	database := client.Database("whatsapp_bulk")
	return &DB{client: client, mdb: database}, nil
}

// EnsureIndexes creates the necessary indexes (idempotent).
func (d *DB) EnsureIndexes(ctx context.Context) error {
	_, err := d.Users().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	// One subscription per user
	_, err = d.Subscriptions().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	// Payment indexes
	_, err = d.Payments().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "txn_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	_, err = d.Payments().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}},
	})
	if err != nil {
		return err
	}
	// Contacts book: one phone per user
	_, err = d.Contacts().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "phone", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	// Email OTPs: one pending OTP per email (upsert replaces on resend)
	_, err = d.EmailOTPs().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	// TTL index: MongoDB auto-deletes OTP docs after expires_at
	expireAfter := int32(0)
	_, err = d.EmailOTPs().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(expireAfter),
	})
	if err != nil {
		return err
	}
	// Password resets: one pending reset per email
	_, err = d.PasswordResets().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return err
	}
	_, err = d.PasswordResets().Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(expireAfter),
	})
	return err
}

func (d *DB) MongoDB() *mongo.Database           { return d.mdb }
func (d *DB) Users() *mongo.Collection         { return d.mdb.Collection("users") }
func (d *DB) Templates() *mongo.Collection     { return d.mdb.Collection("templates") }
func (d *DB) WASessions() *mongo.Collection    { return d.mdb.Collection("wa_sessions") }
func (d *DB) Subscriptions() *mongo.Collection { return d.mdb.Collection("subscriptions") }
func (d *DB) Payments() *mongo.Collection      { return d.mdb.Collection("payments") }
func (d *DB) Contacts() *mongo.Collection      { return d.mdb.Collection("contacts") }
func (d *DB) EmailOTPs() *mongo.Collection       { return d.mdb.Collection("email_otps") }
func (d *DB) PasswordResets() *mongo.Collection { return d.mdb.Collection("password_resets") }

func (d *DB) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}
