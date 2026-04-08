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

type userDoc struct {
	ID        primitive.ObjectID `bson:"_id"`
	Email     string             `bson:"email"`
	Name      string             `bson:"name"`
	Role      string             `bson:"role"`
	CreatedAt interface{}        `bson:"created_at"`
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

	cursor, err := database.Users().Find(context.Background(), bson.M{})
	if err != nil {
		fmt.Printf("Failed to query users: %v\n", err)
		os.Exit(1)
	}
	defer cursor.Close(context.Background())

	fmt.Println("Users in database:")
	fmt.Println("----------------------------------------")
	fmt.Printf("%-26s %-25s %-20s %-10s\n", "ID", "Email", "Name", "Role")
	fmt.Println("----------------------------------------")

	for cursor.Next(context.Background()) {
		var u userDoc
		if err := cursor.Decode(&u); err != nil {
			continue
		}
		fmt.Printf("%-26s %-25s %-20s %-10s\n", u.ID.Hex(), u.Email, u.Name, u.Role)
	}
	fmt.Println("----------------------------------------")
}
