package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

const (
	applicationID = 0x424d424d
	schemaVersion = 1
)

func Open(path string) (*sql.DB, error) {
	newDatabase, err := isNewDatabase(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if !newDatabase {
		if err := validateSchemaVersion(db, path); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000;`); err != nil {
		db.Close()
		return nil, err
	}
	if newDatabase {
		if err := initializeSchema(db, path); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

func isNewDatabase(path string) (bool, error) {
	if path == ":memory:" {
		return true, nil
	}
	_, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect database %q: %w", path, err)
	}
	return false, nil
}

func validateSchemaVersion(db *sql.DB, path string) error {
	var actualApplicationID int
	var actualSchemaVersion int
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&actualApplicationID); err != nil {
		return fmt.Errorf("read application id for database %q: %w", path, err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&actualSchemaVersion); err != nil {
		return fmt.Errorf("read schema version for database %q: %w", path, err)
	}
	if actualApplicationID == applicationID && actualSchemaVersion == schemaVersion {
		return nil
	}
	reason := "has an incompatible schema"
	switch {
	case actualApplicationID == 0 && actualSchemaVersion == 0:
		reason = "is unversioned"
	case actualApplicationID != applicationID:
		reason = "belongs to another application"
	case actualSchemaVersion < schemaVersion:
		reason = fmt.Sprintf("uses older schema version %d", actualSchemaVersion)
	case actualSchemaVersion > schemaVersion:
		reason = fmt.Sprintf("uses newer schema version %d", actualSchemaVersion)
	}
	return fmt.Errorf(
		"database %q %s; move or remove the database manually, then restart Bulk Mail",
		path,
		reason,
	)
}

func initializeSchema(db *sql.DB, path string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for index, statement := range schemaStatements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("schema statement %d: %w", index+1, err)
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA application_id = %d", applicationID)); err != nil {
		return fmt.Errorf("stamp database application id: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
		return fmt.Errorf("stamp database schema version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit database schema: %w", err)
	}
	return validateSchemaVersion(db, path)
}

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`,
	`CREATE TABLE IF NOT EXISTS smtp_profiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		profile_type TEXT NOT NULL,
		host TEXT NOT NULL,
		port INTEGER NOT NULL,
		tls_mode TEXT NOT NULL,
		username TEXT NOT NULL DEFAULT '',
		sender_email TEXT NOT NULL DEFAULT '',
		sender_name TEXT NOT NULL DEFAULT '',
		reply_to TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`,
	`CREATE TABLE IF NOT EXISTS address_lists (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'manual',
		notes TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`,
	`CREATE TABLE IF NOT EXISTS address_list_fields (
		address_list_id INTEGER NOT NULL REFERENCES address_lists(id) ON DELETE CASCADE,
		key TEXT NOT NULL,
		label TEXT NOT NULL,
		role TEXT,
		position INTEGER NOT NULL,
		PRIMARY KEY (address_list_id, key),
		UNIQUE (address_list_id, role),
		UNIQUE (address_list_id, position)
	);`,
	`CREATE TABLE IF NOT EXISTS address_list_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		address_list_id INTEGER NOT NULL REFERENCES address_lists(id) ON DELETE CASCADE,
		email TEXT NOT NULL,
		fields_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS address_list_entries_address_list_email_unique
		ON address_list_entries(address_list_id, lower(email));`,
	`CREATE TABLE IF NOT EXISTS campaigns (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		address_list_id INTEGER REFERENCES address_lists(id) ON DELETE SET NULL,
		profile_id INTEGER REFERENCES smtp_profiles(id) ON DELETE SET NULL,
		subject TEXT NOT NULL DEFAULT '',
		body TEXT NOT NULL DEFAULT '',
		html_body TEXT NOT NULL DEFAULT '',
		request_delivery_notice INTEGER NOT NULL DEFAULT 0,
		remove_diacritics INTEGER NOT NULL DEFAULT 0,
		first_name_format TEXT NOT NULL DEFAULT 'preserve',
		last_name_format TEXT NOT NULL DEFAULT 'preserve',
		full_name_format TEXT NOT NULL DEFAULT 'preserve',
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	);`,
	`CREATE TABLE IF NOT EXISTS campaign_attachments (
			campaign_id INTEGER NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
			position INTEGER NOT NULL,
			filename TEXT NOT NULL,
			output_filename TEXT NOT NULL DEFAULT '',
			content BLOB NOT NULL,
			PRIMARY KEY (campaign_id, position)
	);`,
	`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		campaign_id INTEGER REFERENCES campaigns(id) ON DELETE SET NULL,
		status TEXT NOT NULL,
			metadata_json TEXT NOT NULL,
			total INTEGER NOT NULL DEFAULT 0,
			sent INTEGER NOT NULL DEFAULT 0,
			failed INTEGER NOT NULL DEFAULT 0,
			skipped INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
	`CREATE INDEX IF NOT EXISTS tasks_status_id
			ON tasks(status, id);`,
	`CREATE TABLE IF NOT EXISTS task_inputs (
			task_id INTEGER PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
			profile_id INTEGER REFERENCES smtp_profiles(id) ON DELETE RESTRICT,
			storage_key TEXT NOT NULL UNIQUE
		);`,
	`CREATE TABLE IF NOT EXISTS profile_credentials (
			profile_id INTEGER NOT NULL REFERENCES smtp_profiles(id) ON DELETE CASCADE,
			credential_type TEXT NOT NULL,
			scheme TEXT NOT NULL,
			sealed_value BLOB NOT NULL,
			PRIMARY KEY(profile_id, credential_type)
			);`,
	`CREATE TABLE IF NOT EXISTS message_deliveries (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
				campaign_id INTEGER REFERENCES campaigns(id) ON DELETE SET NULL,
				address_entry_id INTEGER REFERENCES address_list_entries(id) ON DELETE SET NULL,
				email TEXT NOT NULL,
				status TEXT NOT NULL,
				attempt INTEGER NOT NULL DEFAULT 1,
				provider_message_id TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			);`,
	`CREATE TABLE IF NOT EXISTS suppressions (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				email TEXT NOT NULL,
				reason TEXT NOT NULL DEFAULT 'manual',
				created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(email)
			);`,
}
