package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/service"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
	"github.com/joho/godotenv"
)

func main() {
	email := flag.String("email", "", "User email (required)")
	password := flag.String("password", "", "User password (required)")
	name := flag.String("name", "", "User name (required)")
	flag.Parse()

	if *email == "" || *password == "" || *name == "" {
		fmt.Println("Error: email, password, and name are required")
		fmt.Println("Usage: go run cmd/create_user/main.go -email user@example.com -password 123456 -name 'John Doe'")
		os.Exit(1)
	}

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

	authService := service.NewAuthService(database)
	resp, err := authService.Register(context.Background(), types.RegisterRequest{
		Email:    *email,
		Password: *password,
		Name:     *name,
	})
	if err != nil {
		fmt.Printf("Failed to create user: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User created successfully!\n")
	fmt.Printf("  ID:    %s\n", resp.User.ID)
	fmt.Printf("  Email: %s\n", resp.User.Email)
	fmt.Printf("  Name:  %s\n", resp.User.Name)
}
