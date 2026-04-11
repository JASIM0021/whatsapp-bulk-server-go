package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"github.com/joho/godotenv"
)

func main() {
	email := flag.String("email", "", "Admin email (required)")
	password := flag.String("password", "", "Admin password (required)")
	name := flag.String("name", "", "Admin name (required)")
	promote := flag.String("promote", "", "Promote an existing user to admin by email (skips creation)")
	flag.Parse()

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

	ctx := context.Background()

	// Mode 1: Promote existing user
	if *promote != "" {
		result, err := database.Users().UpdateOne(ctx,
			bson.M{"email": *promote},
			bson.M{"$set": bson.M{"role": "admin"}},
		)
		if err != nil {
			fmt.Printf("Failed to promote user: %v\n", err)
			os.Exit(1)
		}
		if result.MatchedCount == 0 {
			fmt.Printf("No user found with email: %s\n", *promote)
			os.Exit(1)
		}
		fmt.Printf("✓ User '%s' promoted to admin!\n", *promote)
		return
	}

	// Mode 2: Create new admin user
	if *email == "" || *password == "" || *name == "" {
		fmt.Println("Usage:")
		fmt.Println("  Create new admin:")
		fmt.Println("    go run cmd/create_admin/main.go -email admin@example.com -password secret123 -name 'Admin User'")
		fmt.Println()
		fmt.Println("  Promote existing user:")
		fmt.Println("    go run cmd/create_admin/main.go -promote user@example.com")
		os.Exit(1)
	}

	authService := service.NewAuthService(database)
	subService := service.NewSubscriptionService(database)
	authService.SetSubscriptionService(subService)

	resp, err := authService.Register(ctx, types.RegisterRequest{
		Email:    *email,
		Password: *password,
		Name:     *name,
	})
	if err != nil {
		fmt.Printf("Failed to create user: %v\n", err)
		os.Exit(1)
	}

	// Set role to admin
	oid, _ := primitive.ObjectIDFromHex(resp.User.ID)
	_, err = database.Users().UpdateOne(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"role": "admin"}},
	)
	if err != nil {
		fmt.Printf("User created but failed to set admin role: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Admin user created successfully!\n")
	fmt.Printf("  ID:    %s\n", resp.User.ID)
	fmt.Printf("  Email: %s\n", resp.User.Email)
	fmt.Printf("  Name:  %s\n", resp.User.Name)
	fmt.Printf("  Role:  admin\n")
}
