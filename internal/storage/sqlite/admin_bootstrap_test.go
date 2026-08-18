package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebe542/go-mediaarchive/internal/credential"
	"github.com/ebe542/go-mediaarchive/internal/identity"
	sqlitestore "github.com/ebe542/go-mediaarchive/internal/storage/sqlite"
)

func TestAdminBootstrapCreatesUserAndCredential(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	now := time.Date(2026, time.August, 18, 20, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	adminUser, err := identity.NewUser(
		userID,
		"bootstrap_admin",
		"Bootstrap Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create admin fixture: %v", err)
	}

	passwordCredential, err := credential.NewPasswordCredential(
		userID,
		"$argon2id$v=19$m=19456,t=2,p=1$c2FsdA$aGFzaA",
		now,
	)
	if err != nil {
		t.Fatalf("create credential fixture: %v", err)
	}

	bootstrapRepository := sqlitestore.NewAdminBootstrapRepository(database)

	if err := bootstrapRepository.BootstrapAdmin(
		ctx,
		adminUser,
		passwordCredential,
	); err != nil {
		t.Fatalf("bootstrap administrator: %v", err)
	}

	storedUser, err := sqlitestore.NewUserRepository(database).FindByID(
		ctx,
		userID,
	)
	if err != nil {
		t.Fatalf("find bootstrapped user: %v", err)
	}
	if storedUser != adminUser {
		t.Fatalf(
			"expected stored user %#v, got %#v",
			adminUser,
			storedUser,
		)
	}

	var storedHash string
	err = database.QueryRowContext(
		ctx,
		`
			SELECT password_hash
			FROM password_credentials
			WHERE user_id = ?
		`,
		userID,
	).Scan(&storedHash)
	if err != nil {
		t.Fatalf("find stored credential: %v", err)
	}
	if storedHash != passwordCredential.PasswordHash {
		t.Fatal("expected stored password hash to match")
	}
	err = bootstrapRepository.BootstrapAdmin(
		ctx,
		adminUser,
		passwordCredential,
	)
	if !errors.Is(err, credential.ErrAlreadyBootstrapped) {
		t.Fatalf(
			"expected ErrAlreadyBootstrapped, got %v",
			err,
		)
	}
}

func TestAdminBootstrapRollsBackUserWhenCredentialInsertFails(
	t *testing.T,
) {
	t.Parallel()

	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "mediaarchive.db")

	database, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})

	if err := sqlitestore.Migrate(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	now := time.Date(2026, time.August, 18, 21, 0, 0, 0, time.UTC)
	userID := "123e4567-e89b-12d3-a456-426614174000"

	adminUser, err := identity.NewUser(
		userID,
		"rollback_admin",
		"Rollback Administrator",
		identity.RoleAdmin,
		now,
	)
	if err != nil {
		t.Fatalf("create admin fixture: %v", err)
	}

	invalidCredential := credential.PasswordCredential{
		UserID:       userID,
		PasswordHash: "plaintext-password",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	bootstrapRepository := sqlitestore.NewAdminBootstrapRepository(database)

	err = bootstrapRepository.BootstrapAdmin(
		ctx,
		adminUser,
		invalidCredential,
	)
	if err == nil {
		t.Fatal("expected credential insert to fail")
	}

	_, err = sqlitestore.NewUserRepository(database).FindByID(ctx, userID)
	if !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf(
			"expected rolled-back user to be absent, got %v",
			err,
		)
	}

	var credentialCount int
	err = database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM password_credentials`,
	).Scan(&credentialCount)
	if err != nil {
		t.Fatalf("count password credentials: %v", err)
	}
	if credentialCount != 0 {
		t.Fatalf(
			"expected no stored credentials, got %d",
			credentialCount,
		)
	}
}
