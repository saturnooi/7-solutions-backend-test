package mongodb

import (
	"context"
	"errors"
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

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (user.User, error) {
	return r.findOne(ctx, "find user by email", bson.D{{Key: "email", Value: email}})
}

func (r *UserRepository) FindByID(ctx context.Context, id string) (user.User, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return user.User{}, fmt.Errorf("mongodb: find user by id: %w", user.ErrInvalidID)
	}
	return r.findOne(ctx, "find user by id", bson.D{{Key: "_id", Value: oid}})
}

func (r *UserRepository) findOne(ctx context.Context, operation string, filter bson.D) (user.User, error) {
	var doc userDocument
	err := r.users.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return user.User{}, fmt.Errorf("mongodb: %s: %w", operation, user.ErrNotFound)
	}
	if err != nil {
		return user.User{}, fmt.Errorf("mongodb: %s: %w", operation, err)
	}
	return doc.toDomain(), nil
}
