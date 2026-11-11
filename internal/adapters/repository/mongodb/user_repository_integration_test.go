//go:build integration

package mongodb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
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

func TestUserRepository_FindByEmail(t *testing.T) {
	t.Parallel()

	repo, _ := newTestRepository(t)
	created, err := repo.Create(t.Context(), newTestUser(t, "alice@example.com"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := repo.Create(t.Context(), newTestUser(t, "bob@example.com")); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	tests := []struct {
		name      string
		email     string
		cancelled bool
		want      user.User
		wantErr   error
	}{
		{name: "stored email", email: "alice@example.com", want: created},
		{name: "unknown email", email: "carol@example.com", wantErr: user.ErrNotFound},
		{name: "lookup is exact because callers normalize", email: "Alice@example.com", wantErr: user.ErrNotFound},
		{name: "cancelled context", email: "alice@example.com", cancelled: true, wantErr: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			got, err := repo.FindByEmail(ctx, tt.email)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FindByEmail() error = %v, want %v", err, tt.wantErr)
			}
			if errors.Is(tt.wantErr, context.Canceled) && errors.Is(err, user.ErrNotFound) {
				t.Errorf("FindByEmail() error = %v, must not be reported as not found", err)
			}
			if got.ID != tt.want.ID || got.Name != tt.want.Name || got.Email != tt.want.Email ||
				got.PasswordHash != tt.want.PasswordHash || !got.CreatedAt.Equal(tt.want.CreatedAt) {
				t.Errorf("FindByEmail() = %+v, want %+v", got, tt.want)
			}
			if tt.wantErr == nil && got.CreatedAt.Location() != time.UTC {
				t.Errorf("CreatedAt location = %v, want UTC", got.CreatedAt.Location())
			}
		})
	}
}

func TestUserRepository_FindByID(t *testing.T) {
	t.Parallel()

	repo, _ := newTestRepository(t)
	created, err := repo.Create(t.Context(), newTestUser(t, "alice@example.com"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	tests := []struct {
		name      string
		id        string
		cancelled bool
		want      user.User
		wantErr   error
	}{
		{name: "stored id", id: created.ID, want: created},
		{name: "upper-case hex of the stored id", id: strings.ToUpper(created.ID), want: created},
		{name: "valid id that is not stored", id: bson.NewObjectID().Hex(), wantErr: user.ErrNotFound},
		{name: "cancelled context", id: created.ID, cancelled: true, wantErr: context.Canceled},
		{name: "not hex is rejected before querying", id: "not-an-object-id-at-all!", cancelled: true, wantErr: user.ErrInvalidID},
		{name: "23 characters is rejected before querying", id: created.ID[:23], cancelled: true, wantErr: user.ErrInvalidID},
		{name: "empty id is rejected before querying", id: "", cancelled: true, wantErr: user.ErrInvalidID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			got, err := repo.FindByID(ctx, tt.id)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("FindByID() error = %v, want %v", err, tt.wantErr)
			}
			if got.ID != tt.want.ID || got.Name != tt.want.Name || got.Email != tt.want.Email ||
				got.PasswordHash != tt.want.PasswordHash || !got.CreatedAt.Equal(tt.want.CreatedAt) {
				t.Errorf("FindByID() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUserRepository_List(t *testing.T) {
	t.Parallel()

	repo, _ := newTestRepository(t)
	base := time.Date(2026, time.November, 11, 9, 0, 0, 0, time.UTC)
	var stored []user.User
	for i, spec := range []struct {
		email     string
		createdAt time.Time
	}{
		{email: "oldest@example.com", createdAt: base},
		{email: "tie-first@example.com", createdAt: base.Add(time.Minute)},
		{email: "tie-second@example.com", createdAt: base.Add(time.Minute)},
		{email: "newest@example.com", createdAt: base.Add(2 * time.Minute)},
	} {
		u, err := user.New(fmt.Sprintf("User %d", i), spec.email, "$2a$04$test-hash", spec.createdAt)
		if err != nil {
			t.Fatalf("user.New() error = %v", err)
		}
		created, err := repo.Create(t.Context(), u)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		stored = append(stored, created)
	}
	newestFirst := []user.User{stored[3], stored[2], stored[1], stored[0]}

	tests := []struct {
		name   string
		limit  int
		offset int
		want   []user.User
	}{
		{name: "all users newest first, ties broken by insertion order", limit: 20, offset: 0, want: newestFirst},
		{name: "first page", limit: 2, offset: 0, want: newestFirst[:2]},
		{name: "second page", limit: 2, offset: 2, want: newestFirst[2:]},
		{name: "page cut short at the end", limit: 3, offset: 3, want: newestFirst[3:]},
		{name: "offset past the end", limit: 20, offset: 10, want: []user.User{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := repo.List(t.Context(), tt.limit, tt.offset)

			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if got == nil {
				t.Fatal("List() = nil, want a non-nil slice")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("List() returned %d users (%v), want %d", len(got), emails(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i].ID || got[i].Email != tt.want[i].Email ||
					got[i].PasswordHash != tt.want[i].PasswordHash || !got[i].CreatedAt.Equal(tt.want[i].CreatedAt) {
					t.Errorf("List()[%d] = %+v, want %+v (order %v)", i, got[i], tt.want[i], emails(got))
				}
			}
		})
	}
}

func TestUserRepository_List_ReportsDatabaseErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		seed      bson.D
		cancelled bool
		wantErr   error
	}{
		{name: "cancelled context", cancelled: true, wantErr: context.Canceled},
		{
			name: "stored document that does not decode",
			seed: bson.D{
				{Key: "_id", Value: bson.NewObjectID()},
				{Key: "name", Value: 42},
				{Key: "email", Value: "corrupt@example.com"},
				{Key: "created_at", Value: time.Now()},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			repo, db := newTestRepository(t)
			if tt.seed != nil {
				if _, err := db.Collection(usersCollection).InsertOne(t.Context(), tt.seed); err != nil {
					t.Fatalf("seed document: %v", err)
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelled {
				cancel()
			}

			got, err := repo.List(ctx, 20, 0)

			if err == nil || !strings.HasPrefix(err.Error(), "mongodb: list users:") {
				t.Fatalf("List() error = %v, want a wrapped list error", err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("List() error = %v, want %v", err, tt.wantErr)
			}
			if got != nil {
				t.Errorf("List() = %v, want nil on error", got)
			}
		})
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
	byName := make(map[string]mongo.IndexSpecification, len(specs))
	for _, s := range specs {
		byName[s.Name] = s
	}

	tests := []struct {
		name       string
		wantKeys   string
		wantUnique bool
	}{
		{name: emailIndexName, wantKeys: `{"email": {"$numberInt":"1"}}`, wantUnique: true},
		{name: createdAtIndexName, wantKeys: `{"created_at": {"$numberInt":"-1"},"_id": {"$numberInt":"-1"}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec, ok := byName[tt.name]
			if !ok {
				t.Fatalf("index %q not found in %+v", tt.name, specs)
			}
			if got := spec.KeysDocument.String(); got != tt.wantKeys {
				t.Errorf("index keys = %s, want %s", got, tt.wantKeys)
			}
			if unique := spec.Unique != nil && *spec.Unique; unique != tt.wantUnique {
				t.Errorf("unique = %v, want %v", unique, tt.wantUnique)
			}
		})
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

func emails(users []user.User) []string {
	out := make([]string, len(users))
	for i, u := range users {
		out[i] = u.Email
	}
	return out
}
