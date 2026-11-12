package mongodb

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"user-management-api/internal/domain/user"
)

type userDocument struct {
	ID           bson.ObjectID `bson:"_id"`
	Name         string        `bson:"name"`
	Email        string        `bson:"email"`
	PasswordHash string        `bson:"password_hash"`
	CreatedAt    time.Time     `bson:"created_at"`
}

func newUserDocument(id bson.ObjectID, u user.User) userDocument {
	return userDocument{
		ID:           id,
		Name:         u.Name,
		Email:        u.Email,
		PasswordHash: u.PasswordHash,
		CreatedAt:    u.CreatedAt.UTC(),
	}
}

func (d userDocument) toDomain() user.User {
	return user.User{
		ID:           d.ID.Hex(),
		Name:         d.Name,
		Email:        d.Email,
		PasswordHash: d.PasswordHash,
		CreatedAt:    d.CreatedAt.UTC(),
	}
}
