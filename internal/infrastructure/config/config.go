package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	defaultHTTPAddr      = ":8080"
	defaultGRPCAddr      = ":9090"
	defaultMongoDatabase = "user_management"
	defaultBcryptCost    = 12
	minBcryptCost        = bcrypt.DefaultCost
	maxBcryptCost        = bcrypt.MaxCost
	defaultJWTIssuer     = "user-management-api"
	defaultJWTTTL        = time.Hour
	minJWTSecretBytes    = 32
)

type Config struct {
	HTTPAddr      string
	GRPCAddr      string
	MongoURI      string
	MongoDatabase string
	BcryptCost    int
	JWTSecret     string
	JWTIssuer     string
	JWTTTL        time.Duration
}

func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		HTTPAddr:      valueOrDefault(getenv("HTTP_ADDR"), defaultHTTPAddr),
		GRPCAddr:      valueOrDefault(getenv("GRPC_ADDR"), defaultGRPCAddr),
		MongoURI:      getenv("MONGO_URI"),
		MongoDatabase: valueOrDefault(getenv("MONGO_DATABASE"), defaultMongoDatabase),
		BcryptCost:    defaultBcryptCost,
		JWTSecret:     getenv("JWT_SECRET"),
		JWTIssuer:     valueOrDefault(getenv("JWT_ISSUER"), defaultJWTIssuer),
		JWTTTL:        defaultJWTTTL,
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
	if len(cfg.JWTSecret) < minJWTSecretBytes {
		errs = append(errs, fmt.Errorf("JWT_SECRET is required and must be at least %d bytes", minJWTSecretBytes))
	}
	if raw := getenv("JWT_TTL"); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil || ttl <= 0 {
			errs = append(errs, errors.New("JWT_TTL must be a positive duration such as 15m or 1h"))
		} else {
			cfg.JWTTTL = ttl
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
