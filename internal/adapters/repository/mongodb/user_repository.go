package mongodb

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"user-management-api/internal/domain/user"
)

const (
	usersCollection = "users"
	emailIndexName  = "email_unique"
)

type UserRepository struct {
	users *mongo.Collection
}

func NewUserRepository(db *mongo.Database) *UserRepository {
	return &UserRepository{users: db.Collection(usersCollection)}
}

func (r *UserRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetName(emailIndexName).SetUnique(true),
	})
	if err != nil {
		return fmt.Errorf("mongodb: ensure user indexes: %w", err)
	}
	return nil
}

func (r *UserRepository) Create(ctx context.Context, u user.User) (user.User, error) {
	doc := newUserDocument(bson.NewObjectID(), u)
	if _, err := r.users.InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return user.User{}, fmt.Errorf("mongodb: create user: %w", user.ErrEmailAlreadyExists)
		}
		return user.User{}, fmt.Errorf("mongodb: create user: %w", err)
	}
	return doc.toDomain(), nil
}
