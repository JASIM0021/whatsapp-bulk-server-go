package main

import (
	"context"
	"fmt"
	"os"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/joho/godotenv"
)

const newLimit = 5

// subscriptionPreview is used to list affected subscriptions before updating.
type subscriptionPreview struct {
	ID           primitive.ObjectID `bson:"_id"`
	UserID       primitive.ObjectID `bson:"user_id"`
	Plan         string             `bson:"plan"`
	Status       string             `bson:"status"`
	MessagesUsed int                `bson:"messages_used"`
	MessageLimit int                `bson:"message_limit"`
}

func main() {
	_ = godotenv.Load()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = os.Getenv("MONGODB_URL")
	}
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	database, err := db.New(mongoURI)
	if err != nil {
		fmt.Printf("Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer database.Close(context.Background())

	ctx := context.Background()

	// Find all free-plan subscriptions with message_limit != newLimit (i.e. old 50-credit users)
	filter := bson.M{
		"plan":          "free",
		"message_limit": bson.M{"$gt": newLimit},
	}

	cursor, err := database.Subscriptions().Find(ctx, filter)
	if err != nil {
		fmt.Printf("Failed to query subscriptions: %v\n", err)
		os.Exit(1)
	}
	defer cursor.Close(ctx)

	var affected []subscriptionPreview
	if err := cursor.All(ctx, &affected); err != nil {
		fmt.Printf("Failed to decode subscriptions: %v\n", err)
		os.Exit(1)
	}

	if len(affected) == 0 {
		fmt.Println("No free-plan users with message_limit > 5 found. Nothing to do.")
		return
	}

	fmt.Printf("Found %d free-plan subscription(s) to reset:\n", len(affected))
	fmt.Printf("%-26s  %-8s  %-14s  %-12s  %-8s\n", "SubID", "Plan", "Status", "Used/Limit", "Action")
	fmt.Println("-------------------------------------------------------------------------------------")
	for _, s := range affected {
		fmt.Printf("%-26s  %-8s  %-14s  %4d / %-5d   → limit=%d, used=0, status=active\n",
			s.ID.Hex(), s.Plan, s.Status, s.MessagesUsed, s.MessageLimit, newLimit)
	}
	fmt.Println("-------------------------------------------------------------------------------------")

	fmt.Printf("\nAbout to reset %d subscription(s). Type 'yes' to confirm: ", len(affected))
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "yes" {
		fmt.Println("Aborted.")
		return
	}

	// Bulk update: reset message_limit to newLimit, messages_used to 0, status to active
	result, err := database.Subscriptions().UpdateMany(ctx,
		filter,
		bson.M{"$set": bson.M{
			"message_limit": newLimit,
			"messages_used": 0,
			"status":        "active",
		}},
	)
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nDone. %d subscription(s) updated → message_limit=%d, messages_used=0, status=active\n",
		result.ModifiedCount, newLimit)
}
