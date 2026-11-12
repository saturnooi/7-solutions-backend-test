package database

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type Health struct {
	client *mongo.Client
}

func NewHealth(client *mongo.Client) *Health {
	return &Health{client: client}
}

func (h *Health) Check(ctx context.Context) error {
	if err := h.client.Ping(ctx, readpref.Primary()); err != nil {
		return fmt.Errorf("mongodb: ping: %w", err)
	}
	return nil
}
