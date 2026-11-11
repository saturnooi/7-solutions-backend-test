package config

import "testing"

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr string
	}{
		{
			name: "defaults when only MONGO_URI is set",
			env:  map[string]string{"MONGO_URI": "mongodb://localhost:27017"},
			want: Config{
				HTTPAddr:      ":8080",
				MongoURI:      "mongodb://localhost:27017",
				MongoDatabase: "user_management",
				BcryptCost:    12,
			},
		},
		{
			name: "every variable set",
			env: map[string]string{
				"HTTP_ADDR":      "127.0.0.1:9000",
				"MONGO_URI":      "mongodb://mongo:27017",
				"MONGO_DATABASE": "users_prod",
				"BCRYPT_COST":    "14",
			},
			want: Config{
				HTTPAddr:      "127.0.0.1:9000",
				MongoURI:      "mongodb://mongo:27017",
				MongoDatabase: "users_prod",
				BcryptCost:    14,
			},
		},
		{
			name: "lowest allowed bcrypt cost",
			env:  map[string]string{"MONGO_URI": "mongodb://localhost:27017", "BCRYPT_COST": "10"},
			want: Config{
				HTTPAddr:      ":8080",
				MongoURI:      "mongodb://localhost:27017",
				MongoDatabase: "user_management",
				BcryptCost:    10,
			},
		},
		{
			name:    "missing MONGO_URI",
			env:     map[string]string{},
			wantErr: "config: MONGO_URI is required",
		},
		{
			name:    "bcrypt cost is not a number",
			env:     map[string]string{"MONGO_URI": "mongodb://localhost:27017", "BCRYPT_COST": "high"},
			wantErr: "config: BCRYPT_COST must be an integer from 10 to 31",
		},
		{
			name:    "bcrypt cost below 10",
			env:     map[string]string{"MONGO_URI": "mongodb://localhost:27017", "BCRYPT_COST": "9"},
			wantErr: "config: BCRYPT_COST must be an integer from 10 to 31",
		},
		{
			name:    "bcrypt cost above 31",
			env:     map[string]string{"MONGO_URI": "mongodb://localhost:27017", "BCRYPT_COST": "32"},
			wantErr: "config: BCRYPT_COST must be an integer from 10 to 31",
		},
		{
			name:    "every problem is reported",
			env:     map[string]string{"BCRYPT_COST": "4"},
			wantErr: "config: MONGO_URI is required\nBCRYPT_COST must be an integer from 10 to 31",
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
