package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
)

func backupDatabase(ctx context.Context, filename string, out io.Writer) error {
	path, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	db, err := sql.Open("sqlite", sqliteDSN(path, url.Values{"mode": {"ro"}}))
	if err != nil {
		return err
	}
	defer db.Close()

	dir, err := os.MkdirTemp("", "livetraffic-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	snapshot := filepath.Join(dir, "snapshot.db")
	if _, err := db.ExecContext(ctx, "VACUUM main INTO ?", snapshot); err != nil {
		return fmt.Errorf("create database snapshot: %w", err)
	}
	if err := verifySnapshot(ctx, snapshot); err != nil {
		return err
	}

	file, err := os.Open(snapshot)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(out, file)
	return err
}

func restoreDatabase(ctx context.Context, filename string, in io.Reader) error {
	path, err := filepath.Abs(filename)
	if err != nil {
		return err
	}
	if err := requireNewDatabase(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".livetraffic-restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()

	if n, err := io.Copy(file, in); err != nil {
		return err
	} else if n == 0 {
		return errors.New("backup is empty")
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := verifySnapshot(ctx, file.Name()); err != nil {
		return err
	}

	// Publish the verified file atomically and fail if another process
	// has created the destination while the backup was being checked.
	if err := os.Link(file.Name(), path); err != nil {
		return fmt.Errorf("install restored database: %w", err)
	}
	return nil
}

func requireNewDatabase(path string) error {
	for _, suffix := range []string{"", "-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); err == nil {
			return fmt.Errorf("destination %s already exists; restore into a new database", path+suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func verifySnapshot(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", sqliteDSN(path, url.Values{"mode": {"ro"}}))
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("read backup: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("backup failed integrity check: %s", integrity)
	}

	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version != 1 {
		return fmt.Errorf("unsupported backup schema version %d", version)
	}
	const schemaCheck = `
		SELECT id, event_type, events.path, received_at, event_id, processed_at, views
		FROM events CROSS JOIN page_view_counts LIMIT 0`
	rows, err := db.QueryContext(ctx, schemaCheck)
	if err != nil {
		return fmt.Errorf("backup does not have the LiveTraffic schema: %w", err)
	}
	return rows.Close()
}
