package middleware

import (
	"context"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

// AdminOnly returns a middleware that checks whether the authenticated user
// has the "admin" role. It must be applied after the Auth middleware so that
// types.UserIDKey is present in the request context.
func AdminOnly(db *mongo.Database) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(types.UserIDKey).(string)
			if !ok || userID == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"success":false,"error":"unauthorized"}`))
				return
			}

			objectID, err := primitive.ObjectIDFromHex(userID)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"success":false,"error":"Admin access required"}`))
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()

			var result bson.M
			err = db.Collection("users").FindOne(ctx, bson.M{"_id": objectID}).Decode(&result)
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"success":false,"error":"Admin access required"}`))
				return
			}

			role, _ := result["role"].(string)
			if role != "admin" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"success":false,"error":"Admin access required"}`))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
