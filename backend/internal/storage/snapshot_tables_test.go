package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRestoreSnapshotPreservesAdditionalApplicationTables(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, `CREATE TABLE snapshot_extra_table(id TEXT PRIMARY KEY, state TEXT NOT NULL); INSERT INTO snapshot_extra_table VALUES('row-1','sent')`); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "snapshot.db")
	if err := store.SnapshotDB(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE snapshot_extra_table SET state='pending' WHERE id='row-1'`); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreSnapshot(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := store.db.QueryRowContext(ctx, `SELECT state FROM snapshot_extra_table WHERE id='row-1'`).Scan(&state); err != nil || state != "sent" {
		t.Fatalf("application table row was not restored: state=%q err=%v", state, err)
	}
}

func TestRestoreSnapshotRejectsSameTableNamesWithDifferentColumnOrder(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveConfig(ctx, map[string]any{"marker": "live"}); err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "snapshot.db")
	snapshot, err := Open(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.SaveConfig(ctx, map[string]any{"marker": "snapshot"}); err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.db.ExecContext(ctx, `
CREATE TABLE config_reordered(value BLOB NOT NULL,name TEXT PRIMARY KEY);
INSERT INTO config_reordered(value,name) SELECT value,name FROM config;
DROP TABLE config;
ALTER TABLE config_reordered RENAME TO config;`); err != nil {
		_ = snapshot.Close()
		t.Fatal(err)
	}
	if err := snapshot.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.RestoreSnapshot(ctx, snapshotPath); err == nil {
		t.Fatal("restore accepted a same-version snapshot with a different column order")
	}
	config, err := store.GetConfig(ctx)
	if err != nil || config["marker"] != "live" {
		t.Fatalf("rejected snapshot changed live config: %#v, %v", config, err)
	}
}
