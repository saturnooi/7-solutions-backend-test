//go:build integration

package mongodb

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"user-management-api/internal/domain/user"
	"user-management-api/internal/infrastructure/database"
)

func TestUserRepository_Create(t *testing.T) {
	t.Parallel()

	repo, db := newTestRepository(t)
	ctx := t.Context()
	u := newTestUser(t, "alice@example.com")

	got, err := repo.Create(ctx, u)

	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	id, err := bson.ObjectIDFromHex(got.ID)
	if err != nil {
		t.Fatalf("Create() ID = %q is not an ObjectID hex: %v", got.ID, err)
	}
	if got.Name != u.Name || got.Email != u.Email || got.PasswordHash != u.PasswordHash || !got.CreatedAt.Equal(u.CreatedAt) {
		t.Errorf("Create() = %+v, want the input user %+v with an ID", got, u)
	}

	var stored bson.M
	if err := db.Collection(usersCollection).FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&stored); err != nil {
		t.Fatalf("stored document not found: %v", err)
	}
	want := bson.M{
		"_id":           id,
		"name":          u.Name,
		"email":         u.Email,
		"password_hash": u.PasswordHash,
		"created_at":    bson.NewDateTimeFromTime(u.CreatedAt),
	}
	if len(stored) != len(want) {
		t.Errorf("stored document has fields %v, want exactly %v", keys(stored), keys(want))
	}
	for k, v := range want {
		if stored[k] != v {
			t.Errorf("stored %s = %#v, want %#v", k, stored[k], v)
		}
	}
}

func TestUserRepository_Create_RejectsDuplicateEmail(t *testing.T) {
	t.Parallel()

	repo, db := newTestRepository(t)
	ctx := t.Context()
	if _, err := repo.Create(ctx, newTestUser(t, "alice@example.com")); err != nil {
		t.Fatalf("first Create() error = %v, want nil", err)
	}

	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{name: "same email", email: "alice@example.com", wantErr: user.ErrEmailAlreadyExists},
		{name: "same email after normalization", email: "  ALICE@example.com ", wantErr: user.ErrEmailAlreadyExists},
		{name: "different email", email: "bob@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repo.Create(ctx, newTestUser(t, tt.email))

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Create() error = %v, want %v", err, tt.wantErr)
			}
		})
	}

	count, err := db.Collection(usersCollection).CountDocuments(ctx, bson.D{})
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 2 {
		t.Errorf("users stored = %d, want 2", count)
	}
}

func TestUserRepository_Create_ReportsDatabaseErrors(t *testing.T) {
	t.Parallel()

	repo, _ := newTestRepository(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := repo.Create(ctx, newTestUser(t, "alice@example.com"))

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create() error = %v, want context.Canceled", err)
	}
	if errors.Is(err, user.ErrEmailAlreadyExists) {
		t.Errorf("Create() error = %v, must not be reported as a duplicate email", err)
	}
	if got != (user.User{}) {
		t.Errorf("Create() = %+v, want the zero User on error", got)
	}
}

func TestUserRepository_EnsureIndexes(t *testing.T) {
	t.Parallel()

	repo, db := newTestRepository(t)
	ctx := t.Context()

	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("second EnsureIndexes() error = %v, want nil", err)
	}

	specs, err := db.Collection(usersCollection).Indexes().ListSpecifications(ctx)
	if err != nil {
		t.Fatalf("list indexes: %v", err)
	}
	var found *mongo.IndexSpecification
	for _, s := range specs {
		if s.Name == emailIndexName {
			found = &s
		}
	}
	if found == nil {
		t.Fatalf("index %q not found in %+v", emailIndexName, specs)
	}
	if found.Unique == nil || !*found.Unique {
		t.Errorf("index %q is not unique", emailIndexName)
	}
	if got := found.KeysDocument.String(); got != `{"email": {"$numberInt":"1"}}` {
		t.Errorf("index keys = %s, want email ascending", got)
	}
}

func TestUserRepository_EnsureIndexes_FailsOnDuplicateData(t *testing.T) {
	t.Parallel()

	_, db := newTestRepository(t)
	ctx := t.Context()
	if err := db.Collection(usersCollection).Drop(ctx); err != nil {
		t.Fatalf("drop users: %v", err)
	}
	docs := []any{
		bson.D{{Key: "email", Value: "alice@example.com"}},
		bson.D{{Key: "email", Value: "alice@example.com"}},
	}
	if _, err := db.Collection(usersCollection).InsertMany(ctx, docs); err != nil {
		t.Fatalf("seed duplicates: %v", err)
	}

	err := NewUserRepository(db).EnsureIndexes(ctx)

	if err == nil {
		t.Fatal("EnsureIndexes() error = nil, want an error when existing emails are not unique")
	}
}

func newTestRepository(t *testing.T) (*UserRepository, *mongo.Database) {
	t.Helper()

	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	client, err := database.Connect(t.Context(), uri, 5*time.Second)
	if err != nil {
		t.Fatalf("connect to test MongoDB at %s (run `make db-up`): %v", uri, err)
	}

	db := client.Database("user_management_test_" + bson.NewObjectID().Hex())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Drop(ctx); err != nil {
			t.Errorf("drop test database: %v", err)
		}
		if err := client.Disconnect(ctx); err != nil {
			t.Errorf("disconnect: %v", err)
		}
	})

	repo := NewUserRepository(db)
	if err := repo.EnsureIndexes(t.Context()); err != nil {
		t.Fatalf("EnsureIndexes() error = %v", err)
	}
	return repo, db
}

func newTestUser(t *testing.T, email string) user.User {
	t.Helper()

	u, err := user.New("Alice Smith", email, "$2a$04$test-hash", time.Now())
	if err != nil {
		t.Fatalf("user.New() error = %v", err)
	}
	return u
}

func keys(m bson.M) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
