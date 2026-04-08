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
	return err
}

func (d *DB) Users() *mongo.Collection      { return d.mdb.Collection("users") }
func (d *DB) Templates() *mongo.Collection  { return d.mdb.Collection("templates") }
func (d *DB) WASessions() *mongo.Collection { return d.mdb.Collection("wa_sessions") }

func (d *DB) Close(ctx context.Context) error {
	return d.client.Disconnect(ctx)
}
