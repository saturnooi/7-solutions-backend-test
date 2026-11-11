package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	secret := strings.Repeat("s", 32)
	required := func(extra map[string]string) map[string]string {
		env := map[string]string{"MONGO_URI": "mongodb://localhost:27017", "JWT_SECRET": secret}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}
	defaults := Config{
		HTTPAddr:      ":8080",
		GRPCAddr:      ":9090",
		MongoURI:      "mongodb://localhost:27017",
		MongoDatabase: "user_management",
		BcryptCost:    12,
		JWTSecret:     secret,
		JWTIssuer:     "user-management-api",
		JWTTTL:        time.Hour,
	}
	with := func(change func(*Config)) Config {
		c := defaults
		change(&c)
		return c
	}

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "defaults when only required variables are set",
			env:  required(nil),
			want: defaults,
		},
		{
			name: "every variable set",
			env: map[string]string{
				"HTTP_ADDR":      "127.0.0.1:9000",
				"GRPC_ADDR":      "127.0.0.1:9001",
				"MONGO_URI":      "mongodb://mongo:27017",
				"MONGO_DATABASE": "users_prod",
				"BCRYPT_COST":    "14",
				"JWT_SECRET":     secret + "-longer",
				"JWT_ISSUER":     "users.example.com",
				"JWT_TTL":        "15m",
			},
			want: Config{
				HTTPAddr:      "127.0.0.1:9000",
				GRPCAddr:      "127.0.0.1:9001",
				MongoURI:      "mongodb://mongo:27017",
				MongoDatabase: "users_prod",
				BcryptCost:    14,
				JWTSecret:     secret + "-longer",
				JWTIssuer:     "users.example.com",
				JWTTTL:        15 * time.Minute,
			},
		},
		{
			name: "lowest allowed bcrypt cost",
			env:  required(map[string]string{"BCRYPT_COST": "10"}),
			want: with(func(c *Config) { c.BcryptCost = 10 }),
		},
		{
			name:    "missing MONGO_URI",
			env:     map[string]string{"JWT_SECRET": secret},
			wantErr: "config: MONGO_URI is required",
		},
		{
			name:    "bcrypt cost is not a number",
			env:     required(map[string]string{"BCRYPT_COST": "high"}),
			wantErr: "config: BCRYPT_COST must be an integer from 10 to 31",
		},
		{
			name:    "bcrypt cost below 10",
			env:     required(map[string]string{"BCRYPT_COST": "9"}),
			wantErr: "config: BCRYPT_COST must be an integer from 10 to 31",
		},
		{
			name:    "bcrypt cost above 31",
			env:     required(map[string]string{"BCRYPT_COST": "32"}),
			wantErr: "config: BCRYPT_COST must be an integer from 10 to 31",
		},
		{
			name:    "missing JWT_SECRET",
			env:     map[string]string{"MONGO_URI": "mongodb://localhost:27017"},
			wantErr: "config: JWT_SECRET is required and must be at least 32 bytes",
		},
		{
			name:    "JWT_SECRET of 31 bytes",
			env:     required(map[string]string{"JWT_SECRET": strings.Repeat("s", 31)}),
			wantErr: "config: JWT_SECRET is required and must be at least 32 bytes",
		},
		{
			name:    "JWT_TTL is not a duration",
			env:     required(map[string]string{"JWT_TTL": "an hour"}),
			wantErr: "config: JWT_TTL must be a positive duration such as 15m or 1h",
		},
		{
			name:    "JWT_TTL is zero",
			env:     required(map[string]string{"JWT_TTL": "0s"}),
			wantErr: "config: JWT_TTL must be a positive duration such as 15m or 1h",
		},
		{
			name:    "JWT_TTL is negative",
			env:     required(map[string]string{"JWT_TTL": "-1h"}),
			wantErr: "config: JWT_TTL must be a positive duration such as 15m or 1h",
		},
		{
			name: "every problem is reported",
			env:  map[string]string{"BCRYPT_COST": "4", "JWT_TTL": "soon"},
			wantErr: "config: MONGO_URI is required\n" +
				"BCRYPT_COST must be an integer from 10 to 31\n" +
				"JWT_SECRET is required and must be at least 32 bytes\n" +
				"JWT_TTL must be a positive duration such as 15m or 1h",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Load(func(key string) string { return tt.env[key] })

			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("Load() error = %v, want %q", err, tt.wantErr)
				}
				if got != (Config{}) {
					t.Errorf("Load() = %+v, want the zero Config on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestHTTPAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "default", env: map[string]string{}, want: ":8080"},
		{name: "set", env: map[string]string{"HTTP_ADDR": "127.0.0.1:9000"}, want: "127.0.0.1:9000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := HTTPAddr(func(key string) string { return tt.env[key] }); got != tt.want {
				t.Errorf("HTTPAddr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoad_ErrorsNeverContainTheSecret(t *testing.T) {
	t.Parallel()

	const shortSecret = "too-short-secret"

	_, err := Load(func(key string) string {
		return map[string]string{"JWT_SECRET": shortSecret}[key]
	})

	if err == nil {
		t.Fatal("Load() error = nil, want an error")
	}
	if strings.Contains(err.Error(), shortSecret) {
		t.Errorf("Load() error leaks JWT_SECRET: %q", err)
	}
}
