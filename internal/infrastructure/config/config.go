package config

import (
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultHTTPAddr      = ":8080"
	defaultMongoDatabase = "user_management"
	defaultBcryptCost    = 12
	minBcryptCost        = bcrypt.DefaultCost
	maxBcryptCost        = bcrypt.MaxCost
)

type Config struct {
	HTTPAddr      string
	MongoURI      string
	MongoDatabase string
	BcryptCost    int
}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:      valueOrDefault(getenv("HTTP_ADDR"), defaultHTTPAddr),
		MongoURI:      getenv("MONGO_URI"),
		MongoDatabase: valueOrDefault(getenv("MONGO_DATABASE"), defaultMongoDatabase),
		BcryptCost:    defaultBcryptCost,
	}

	var errs []error
	if cfg.MongoURI == "" {
		errs = append(errs, errors.New("MONGO_URI is required"))
	}
	if raw := getenv("BCRYPT_COST"); raw != "" {
		cost, err := strconv.Atoi(raw)
		if err != nil || cost < minBcryptCost || cost > maxBcryptCost {
			errs = append(errs, fmt.Errorf("BCRYPT_COST must be an integer from %d to %d", minBcryptCost, maxBcryptCost))
		} else {
			cfg.BcryptCost = cost
		}
	}

	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("config: %w", err)
	}
	return cfg, nil
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
