package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func Connect(ctx context.Context, uri string, timeout time.Duration) (*mongo.Client, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(timeout))
	if err != nil {
		return nil, fmt.Errorf("mongodb: connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		return nil, errors.Join(
			fmt.Errorf("mongodb: ping: %w", err),
			client.Disconnect(context.WithoutCancel(ctx)),
		)
	}
	return client, nil
}
