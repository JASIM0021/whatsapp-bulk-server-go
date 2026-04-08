package service

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/db"
	"github.com/JASIM0021/bulk-whatsapp-send/backend/internal/types"
)

type TemplateService struct {
	db *db.DB
}

func NewTemplateService(database *db.DB) *TemplateService {
	return &TemplateService{db: database}
}

// templateDoc is the MongoDB document shape for the templates collection.
type templateDoc struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"`
	UserID    primitive.ObjectID `bson:"user_id"`
	Name      string             `bson:"name"`
	Category  string             `bson:"category"`
	Body      string             `bson:"body"`
	Variables []string           `bson:"variables"`
	IsDefault bool               `bson:"is_default"`
	CreatedAt time.Time          `bson:"created_at"`
}

func (d templateDoc) toType() types.Template {
	vars := d.Variables
	if vars == nil {
		vars = []string{}
	}
	return types.Template{
		ID:        d.ID.Hex(),
		UserID:    d.UserID.Hex(),
		Name:      d.Name,
		Category:  d.Category,
		Body:      d.Body,
		Variables: vars,
		IsDefault: d.IsDefault,
		CreatedAt: d.CreatedAt.Format(time.RFC3339),
	}
}

var validCategories = map[string]bool{
	"Marketing": true, "Utility": true, "Authentication": true, "Custom": true,
}

func (s *TemplateService) List(ctx context.Context, userID string) ([]types.Template, error) {
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}

	opts := options.Find().SetSort(bson.D{
		{Key: "is_default", Value: -1},
		{Key: "created_at", Value: 1},
	})
	cursor, err := s.db.Templates().Find(ctx, bson.M{"user_id": oid}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []types.Template
	for cursor.Next(ctx) {
		var doc templateDoc
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		results = append(results, doc.toType())
	}
	if results == nil {
		results = []types.Template{}
	}
	return results, nil
}

func (s *TemplateService) Create(ctx context.Context, userID string, req types.CreateTemplateRequest) (*types.Template, error) {
	if !validCategories[req.Category] {
		return nil, fmt.Errorf("invalid category: must be one of Marketing, Utility, Authentication, Custom")
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}
	if req.Variables == nil {
		req.Variables = []string{}
	}
	doc := templateDoc{
		ID:        primitive.NewObjectID(),
		UserID:    oid,
		Name:      req.Name,
		Category:  req.Category,
		Body:      req.Body,
		Variables: req.Variables,
		IsDefault: false,
		CreatedAt: time.Now(),
	}
	if _, err := s.db.Templates().InsertOne(ctx, doc); err != nil {
		return nil, err
	}
	t := doc.toType()
	return &t, nil
}

func (s *TemplateService) Update(ctx context.Context, userID, templateID string, req types.UpdateTemplateRequest) (*types.Template, error) {
	if !validCategories[req.Category] {
		return nil, fmt.Errorf("invalid category: must be one of Marketing, Utility, Authentication, Custom")
	}
	tid, err := primitive.ObjectIDFromHex(templateID)
	if err != nil {
		return nil, fmt.Errorf("invalid template ID")
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID")
	}
	if req.Variables == nil {
		req.Variables = []string{}
	}

	after := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc templateDoc
	err = s.db.Templates().FindOneAndUpdate(ctx,
		bson.M{"_id": tid, "user_id": uid},
		bson.M{"$set": bson.M{
			"name":      req.Name,
			"category":  req.Category,
			"body":      req.Body,
			"variables": req.Variables,
		}},
		after,
	).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("template not found")
	}
	if err != nil {
		return nil, err
	}
	t := doc.toType()
	return &t, nil
}

func (s *TemplateService) Delete(ctx context.Context, userID, templateID string) error {
	tid, err := primitive.ObjectIDFromHex(templateID)
	if err != nil {
		return fmt.Errorf("invalid template ID")
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return fmt.Errorf("invalid user ID")
	}

	// Check if default
	var doc templateDoc
	if err := s.db.Templates().FindOne(ctx, bson.M{"_id": tid, "user_id": uid}).Decode(&doc); err != nil {
		return fmt.Errorf("template not found")
	}
	if doc.IsDefault {
		return fmt.Errorf("cannot delete default template")
	}
	_, err = s.db.Templates().DeleteOne(ctx, bson.M{"_id": tid, "user_id": uid})
	return err
}

func (s *TemplateService) seedDefaultTemplates(ctx context.Context, userID string) error {
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}

	defaults := []templateDoc{
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Eid Special Offer", Category: "Marketing", Body: "Eid Mubarak {{name}}! 🌙✨ Celebrate this special occasion with {{discount}}% OFF on everything! Limited time offer — valid till {{expiry}}. Shop now and enjoy the festivities! 🛍️", Variables: []string{"name", "discount", "expiry"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Diwali Celebration Sale", Category: "Marketing", Body: "Happy Diwali {{name}}! 🪔🎉 Light up your celebrations with our BIGGEST SALE of the year — {{discount}}% OFF! Use code *{{offerCode}}* at checkout. Offer ends tonight! 🎁", Variables: []string{"name", "discount", "offerCode"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Flash Sale Alert", Category: "Marketing", Body: "⚡ FLASH SALE! Hey {{name}}, grab {{discount}}% OFF on ALL products RIGHT NOW! ⚡ This deal EXPIRES at {{endTime}}. Don't miss out — shop before it's gone! 🔥", Variables: []string{"name", "discount", "endTime"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "New Year Special Offer", Category: "Marketing", Body: "Happy New Year {{name}}! 🎆🥂 Start the year with BIG savings — {{discount}}% OFF storewide! Valid until {{validity}}. Your best year starts with the best deals! 🎉", Variables: []string{"name", "discount", "validity"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Weekend Mega Sale", Category: "Marketing", Body: "Hey {{name}}! 🛒 Weekend MEGA SALE is HERE! Get {{discount}}% OFF on {{category}}. Saturday & Sunday only — prices go back up Monday! Hurry, limited stock! 🔥", Variables: []string{"name", "discount", "category"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "VIP Customer Exclusive", Category: "Marketing", Body: "Exclusive for YOU, {{name}}! 👑 As our VIP customer, enjoy a special {{discount}}% discount just for you. Use code *{{offerCode}}* — this offer is NOT available to others. Valid 48 hrs only! 💎", Variables: []string{"name", "offerCode", "discount"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "New Product Launch", Category: "Marketing", Body: "Big news {{name}}! 🚀 We just launched *{{productName}}* and it's incredible! Be among the FIRST to get it — exclusive launch discount available now. Check it out: {{link}} 🎉", Variables: []string{"name", "productName", "link"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Grand Anniversary Sale", Category: "Marketing", Body: "🎂 We're celebrating {{years}} years in business and YOU are the reason! Thank you {{name}}! To celebrate, enjoy {{discount}}% OFF everything today. Our anniversary gift to you! 🎊", Variables: []string{"name", "years", "discount"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Clearance Blowout", Category: "Marketing", Body: "CLEARANCE SALE {{name}}! 🏷️ Up to {{discount}}% OFF on {{category}} — we're clearing out stock and the savings are INSANE! First come first served. Shop before stock runs out! 💥", Variables: []string{"name", "discount", "category"}, IsDefault: true, CreatedAt: time.Now()},
		{ID: primitive.NewObjectID(), UserID: uid, Name: "Referral Reward Offer", Category: "Marketing", Body: "Hey {{name}}! 🎁 Share your special code *{{referralCode}}* with friends and you BOTH get {{reward}}! The more you share, the more you earn. Start sharing now! 🤝", Variables: []string{"name", "referralCode", "reward"}, IsDefault: true, CreatedAt: time.Now()},
	}

	docs := make([]interface{}, len(defaults))
	for i, d := range defaults {
		docs[i] = d
	}
	_, err = s.db.Templates().InsertMany(ctx, docs)
	return err
}
