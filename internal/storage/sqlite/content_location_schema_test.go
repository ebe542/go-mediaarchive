package sqlite_test

import "testing"

func TestContentLocationMigrationCreatesExpectedTable(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)

	var count int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'media_contents'`,
	).Scan(&count); err != nil {
		t.Fatalf("find media_contents table: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected media_contents table, got %d", count)
	}
}

func TestContentLocationSchemaRejectsUnsafeKeys(t *testing.T) {
	for name, storageKey := range map[string]string{
		"empty":             "",
		"absolute":          "/32/content",
		"parent traversal":  "32/../content",
		"current directory": "32/./content",
		"Windows separator": `32\content`,
		"drive separator":   "C:/content",
	} {
		t.Run(name, func(t *testing.T) {
			ctx, database := openMediaSchemaDatabase(t)
			insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
			insertValidSchemaMedia(t, ctx, database)

			if _, err := database.ExecContext(
				ctx,
				`INSERT INTO media_contents (media_id, storage_key, stored_at) VALUES (?, ?, ?)`,
				schemaMediaID,
				storageKey,
				schemaTimestamp,
			); err == nil {
				t.Fatalf("expected storage key %q to be rejected", storageKey)
			}
		})
	}
}

func TestContentLocationSchemaRestrictsMediaDeletion(t *testing.T) {
	ctx, database := openMediaSchemaDatabase(t)
	insertMediaSchemaUser(t, ctx, database, schemaOwnerID, "media_owner")
	insertValidSchemaMedia(t, ctx, database)
	if _, err := database.ExecContext(
		ctx,
		`INSERT INTO media_contents (media_id, storage_key, stored_at) VALUES (?, ?, ?)`,
		schemaMediaID,
		"32/"+schemaMediaID,
		schemaTimestamp,
	); err != nil {
		t.Fatalf("insert content location: %v", err)
	}

	if _, err := database.ExecContext(
		ctx,
		`DELETE FROM media_items WHERE id = ?`,
		schemaMediaID,
	); err == nil {
		t.Fatal("expected referenced media deletion to be restricted")
	}
}
