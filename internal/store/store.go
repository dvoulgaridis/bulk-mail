package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/mail"
	taskpkg "github.com/dvoulgaridis/bulk-mail/internal/tasks"
	"github.com/dvoulgaridis/bulk-mail/internal/validation"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/unicode/norm"
)

// Store construction and application state.

type Store struct {
	db *sql.DB
}

var (
	ErrAddressListInUse = errors.New("address list is used by a saved campaign")
	ErrCampaignInUse    = errors.New("campaign is used by a queued or active task")
	ErrProfileInUse     = errors.New("profile is used by a queued or active task")
)

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Settings.

func (s *Store) SeedDefaults(ctx context.Context) error {
	settings := DefaultAppSettings()
	values := map[string]string{
		"theme":                        settings.Theme,
		"email_rate_per_min":           strconv.Itoa(settings.EmailRatePerMin),
		"email_interval_ms":            strconv.Itoa(settings.EmailIntervalMs),
		"max_campaign_address_entries": strconv.Itoa(settings.MaxCampaignAddressEntries),
		"max_campaign_documents":       strconv.Itoa(settings.MaxCampaignDocuments),
	}
	for key, value := range values {
		if _, err := s.db.ExecContext(
			ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`,
			key,
			value,
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetSetting(ctx context.Context, key, fallback string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	return value, err
}

func (s *Store) GetAppSettings(ctx context.Context) (AppSettings, error) {
	settings := DefaultAppSettings()
	theme, err := s.GetSetting(ctx, "theme", settings.Theme)
	if err != nil {
		return AppSettings{}, err
	}
	settings.Theme = theme
	if settings.EmailRatePerMin, err = s.intSetting(ctx, "email_rate_per_min", settings.EmailRatePerMin); err != nil {
		return AppSettings{}, err
	}
	if settings.EmailIntervalMs, err = s.intSetting(ctx, "email_interval_ms", settings.EmailIntervalMs); err != nil {
		return AppSettings{}, err
	}
	if settings.MaxCampaignAddressEntries, err = s.intSetting(
		ctx,
		"max_campaign_address_entries",
		settings.MaxCampaignAddressEntries,
	); err != nil {
		return AppSettings{}, err
	}
	if settings.MaxCampaignDocuments, err = s.intSetting(
		ctx,
		"max_campaign_documents",
		settings.MaxCampaignDocuments,
	); err != nil {
		return AppSettings{}, err
	}
	if err := settings.Validate(); err != nil {
		return AppSettings{}, err
	}
	return settings, nil
}

func (s *Store) intSetting(ctx context.Context, key string, defaultValue int) (int, error) {
	value, err := s.GetSetting(ctx, key, strconv.Itoa(defaultValue))
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("setting %s must be an integer", key)
	}
	return parsed, nil
}

func (s *Store) SaveAppSettings(ctx context.Context, settings AppSettings) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	values := map[string]string{
		"theme":                        settings.Theme,
		"email_rate_per_min":           strconv.Itoa(settings.EmailRatePerMin),
		"email_interval_ms":            strconv.Itoa(settings.EmailIntervalMs),
		"max_campaign_address_entries": strconv.Itoa(settings.MaxCampaignAddressEntries),
		"max_campaign_documents":       strconv.Itoa(settings.MaxCampaignDocuments),
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP
		`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) State(ctx context.Context) (AppState, error) {
	settings, err := s.GetAppSettings(ctx)
	if err != nil {
		return AppState{}, err
	}
	profiles, err := s.ListSMTPProfiles(ctx)
	if err != nil {
		return AppState{}, err
	}
	lists, err := s.ListAddressLists(ctx)
	if err != nil {
		return AppState{}, err
	}
	campaigns, err := s.ListCampaigns(ctx)
	if err != nil {
		return AppState{}, err
	}
	suppressions, err := s.ListSuppressions(ctx)
	if err != nil {
		return AppState{}, err
	}
	profiles = emptyIfNil(profiles)
	lists = emptyIfNil(lists)
	campaigns = emptyIfNil(campaigns)
	suppressions = emptyIfNil(suppressions)
	return AppState{
		Settings:             settings,
		AddressFieldDefaults: DefaultAddressFields(),
		SMTPProfiles:         profiles,
		AddressLists:         lists,
		Campaigns:            campaigns,
		Suppressions:         suppressions,
	}, nil
}

// Sender profiles and credentials.

func (s *Store) ListSMTPProfiles(ctx context.Context) ([]SMTPProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, p.profile_type, p.host, p.port, p.tls_mode, p.username,
		       p.sender_email, p.sender_name, p.reply_to,
		       EXISTS(SELECT 1 FROM profile_credentials c
		              WHERE c.profile_id = p.id AND c.credential_type = ?) AS password_exists,
		       EXISTS(SELECT 1 FROM profile_credentials c
		              WHERE c.profile_id = p.id AND c.credential_type = ?) AS has_google_oauth,
		       p.created_at, p.updated_at
		FROM smtp_profiles p ORDER BY p.updated_at DESC, p.id DESC
	`, CredentialSMTPPassword, CredentialGmailRefreshToken)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var profiles []SMTPProfile
	for rows.Next() {
		var p SMTPProfile
		if err := rows.Scan(
			&p.ID,
			&p.Name,
			&p.ProfileType,
			&p.Host,
			&p.Port,
			&p.TLSMode,
			&p.Username,
			&p.SenderEmail,
			&p.SenderName,
			&p.ReplyTo,
			&p.PasswordExists,
			&p.HasGoogleOAuth,
			&p.CreatedAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		profiles = append(profiles, p)
	}
	return profiles, rows.Err()
}

func (s *Store) SaveSMTPProfileWithCredentialChanges(
	ctx context.Context,
	p SMTPProfile,
	credentials []ProfileCredential,
	deletedCredentialTypes []string,
) (SMTPProfile, error) {
	p, err := NormalizeSMTPProfile(p)
	if err != nil {
		return SMTPProfile{}, err
	}
	if err := validateCredentialChanges(credentials, deletedCredentialTypes); err != nil {
		return SMTPProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SMTPProfile{}, err
	}
	defer tx.Rollback()
	if p.ID > 0 {
		if err := ensureProfileMutable(ctx, tx, p.ID); err != nil {
			return SMTPProfile{}, err
		}
	}
	p, err = saveSMTPProfile(ctx, tx, p)
	if err != nil {
		return SMTPProfile{}, err
	}
	for _, credential := range credentials {
		credential.ProfileID = p.ID
		if err := saveProfileCredential(ctx, tx, credential); err != nil {
			return SMTPProfile{}, err
		}
	}
	for _, credentialType := range deletedCredentialTypes {
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM profile_credentials WHERE profile_id = ? AND credential_type = ?`,
			p.ID,
			strings.TrimSpace(credentialType),
		); err != nil {
			return SMTPProfile{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SMTPProfile{}, err
	}
	return s.GetSMTPProfile(ctx, p.ID)
}

func validateCredentialChanges(credentials []ProfileCredential, deletedCredentialTypes []string) error {
	changed := make(map[string]struct{}, len(credentials))
	for _, credential := range credentials {
		credentialType := strings.TrimSpace(credential.CredentialType)
		changed[credentialType] = struct{}{}
	}
	for _, credentialType := range deletedCredentialTypes {
		credentialType = strings.TrimSpace(credentialType)
		if _, exists := changed[credentialType]; exists {
			return fmt.Errorf("credential type %q cannot be changed and deleted together", credentialType)
		}
	}
	return nil
}

type profileExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func saveSMTPProfile(ctx context.Context, db profileExecutor, p SMTPProfile) (SMTPProfile, error) {
	if p.ID == 0 {
		res, err := db.ExecContext(ctx, `
			INSERT INTO smtp_profiles
				(name, profile_type, host, port, tls_mode, username, sender_email, sender_name, reply_to)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, p.Name, p.ProfileType, p.Host, p.Port, p.TLSMode, p.Username, p.SenderEmail, p.SenderName, p.ReplyTo)
		if err != nil {
			return SMTPProfile{}, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return SMTPProfile{}, err
		}
		p.ID = id
	} else {
		_, err := db.ExecContext(ctx, `
			UPDATE smtp_profiles
			SET name = ?, profile_type = ?, host = ?, port = ?, tls_mode = ?, username = ?, sender_email = ?,
			    sender_name = ?, reply_to = ?, updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, p.Name, p.ProfileType, p.Host, p.Port, p.TLSMode, p.Username, p.SenderEmail, p.SenderName, p.ReplyTo, p.ID)
		if err != nil {
			return SMTPProfile{}, err
		}
	}
	return p, nil
}

func NormalizeSMTPProfile(p SMTPProfile) (SMTPProfile, error) {
	var err error
	if p.Name, err = validation.TrimField(p.Name, "profile name"); err != nil {
		return SMTPProfile{}, err
	}
	profileType, err := validation.TrimField(strings.ToLower(string(p.ProfileType)), "profile type")
	if err != nil {
		return SMTPProfile{}, err
	}
	p.ProfileType = ProfileType(profileType)
	if p.Host, err = validation.TrimField(p.Host, "SMTP host"); err != nil {
		return SMTPProfile{}, err
	}
	if p.Username, err = validation.TrimField(p.Username, "SMTP username"); err != nil {
		return SMTPProfile{}, err
	}
	if p.SenderName, err = validation.TrimField(p.SenderName, "sender name"); err != nil {
		return SMTPProfile{}, err
	}
	if p.Name == "" {
		return SMTPProfile{}, errors.New("profile name is required")
	}
	switch p.ProfileType {
	case ProfileTypeSMTP, ProfileTypeGmailAppPassword, ProfileTypeGmailOAuth:
	default:
		return SMTPProfile{}, errors.New("profile type must be SMTP, Gmail App Password, or Gmail OAuth")
	}
	p.SenderEmail, err = validation.NormalizeEmail(p.SenderEmail)
	if err != nil {
		return SMTPProfile{}, fmt.Errorf("sender email: %w", err)
	}
	if strings.TrimSpace(p.ReplyTo) != "" {
		p.ReplyTo, err = validation.NormalizeEmail(p.ReplyTo)
		if err != nil {
			return SMTPProfile{}, fmt.Errorf("reply-to email: %w", err)
		}
	} else {
		p.ReplyTo = ""
	}
	if p.ProfileType == ProfileTypeGmailOAuth {
		p.Host = ""
		p.Port = 587
		p.TLSMode = "starttls"
		p.Username = ""
	} else {
		if p.Host == "" {
			return SMTPProfile{}, errors.New("SMTP host is required")
		}
		if p.Port == 0 {
			p.Port = 587
		}
		if p.Port < 1 || p.Port > 65535 {
			return SMTPProfile{}, errors.New("SMTP port must be between 1 and 65535")
		}
		p.TLSMode = strings.ToLower(strings.TrimSpace(p.TLSMode))
		if p.Port == 465 {
			p.TLSMode = "tls"
		} else if p.TLSMode == "" {
			p.TLSMode = "starttls"
		}
		switch p.TLSMode {
		case "none", "starttls", "tls":
		default:
			return SMTPProfile{}, errors.New("SMTP security must be none, STARTTLS, or TLS")
		}
	}
	return p, nil
}

func (s *Store) GetSMTPProfile(ctx context.Context, id int64) (SMTPProfile, error) {
	return getSMTPProfile(ctx, s.db, id)
}

func getSMTPProfile(ctx context.Context, db profileExecutor, id int64) (SMTPProfile, error) {
	var p SMTPProfile
	err := db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.profile_type, p.host, p.port, p.tls_mode, p.username,
		       p.sender_email, p.sender_name, p.reply_to,
		       EXISTS(SELECT 1 FROM profile_credentials c
		              WHERE c.profile_id = p.id AND c.credential_type = ?) AS password_exists,
		       EXISTS(SELECT 1 FROM profile_credentials c
		              WHERE c.profile_id = p.id AND c.credential_type = ?) AS has_google_oauth,
		       p.created_at, p.updated_at
		FROM smtp_profiles p WHERE p.id = ?
	`, CredentialSMTPPassword, CredentialGmailRefreshToken, id).Scan(
		&p.ID,
		&p.Name,
		&p.ProfileType,
		&p.Host,
		&p.Port,
		&p.TLSMode,
		&p.Username,
		&p.SenderEmail,
		&p.SenderName,
		&p.ReplyTo,
		&p.PasswordExists,
		&p.HasGoogleOAuth,
		&p.CreatedAt,
		&p.UpdatedAt,
	)
	return p, err
}

func (s *Store) DeleteSMTPProfile(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureProfileMutable(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM smtp_profiles WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CheckSMTPProfileMutable(ctx context.Context, id int64) error {
	return ensureProfileMutable(ctx, s.db, id)
}

func ensureProfileMutable(ctx context.Context, db profileExecutor, id int64) error {
	var inUse bool
	if err := db.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM task_inputs WHERE profile_id = ?)`,
		id,
	).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return ErrProfileInUse
	}
	return nil
}

func saveProfileCredential(ctx context.Context, db profileExecutor, c ProfileCredential) error {
	if c.ProfileID <= 0 {
		return errors.New("profile id is required")
	}
	c.CredentialType = strings.TrimSpace(c.CredentialType)
	if c.CredentialType == "" {
		return errors.New("credential type is required")
	}
	c.Scheme = strings.TrimSpace(c.Scheme)
	if c.Scheme == "" {
		return errors.New("credential encryption scheme is required")
	}
	if len(c.SealedValue) == 0 {
		return errors.New("sealed credential value is required")
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO profile_credentials
			(profile_id, credential_type, scheme, sealed_value)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(profile_id, credential_type) DO UPDATE SET
			scheme = excluded.scheme,
			sealed_value = excluded.sealed_value
	`, c.ProfileID, c.CredentialType, c.Scheme, c.SealedValue)
	return err
}

func (s *Store) GetProfileCredential(
	ctx context.Context,
	profileID int64,
	credentialType string,
) (ProfileCredential, error) {
	var c ProfileCredential
	err := s.db.QueryRowContext(ctx, `
		SELECT profile_id, credential_type, scheme, sealed_value
		FROM profile_credentials
		WHERE profile_id = ? AND credential_type = ?
	`, profileID, credentialType).Scan(
		&c.ProfileID,
		&c.CredentialType,
		&c.Scheme,
		&c.SealedValue,
	)
	return c, err
}

// Address lists, field definitions, and entries.

func (s *Store) CreateAddressList(
	ctx context.Context,
	name string,
	source string,
	notes string,
	definitions []AddressFieldDefinition,
	entries []AddressEntry,
) (AddressList, error) {
	definitions, err := normalizeAddressFieldDefinitions(definitions)
	if err != nil {
		return AddressList{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "Imported addresses"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "manual"
	}
	notes, err = validation.TrimField(notes, "notes")
	if err != nil {
		return AddressList{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AddressList{}, err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(
		ctx,
		`INSERT INTO address_lists (name, source, notes) VALUES (?, ?, ?)`,
		name,
		source,
		notes,
	)
	if err != nil {
		return AddressList{}, err
	}
	listID, err := res.LastInsertId()
	if err != nil {
		return AddressList{}, err
	}
	if err := insertAddressFields(ctx, tx, listID, definitions); err != nil {
		return AddressList{}, err
	}
	if err := insertEntries(ctx, tx, listID, definitions, entries); err != nil {
		return AddressList{}, err
	}
	if err := tx.Commit(); err != nil {
		return AddressList{}, err
	}
	return s.GetAddressList(ctx, listID)
}

func (s *Store) ReplaceAddressList(
	ctx context.Context,
	id int64,
	name string,
	source string,
	notes string,
	definitions []AddressFieldDefinition,
	entries []AddressEntry,
) (AddressList, error) {
	incomingDefinitions, err := normalizeAddressFieldDefinitions(definitions)
	if err != nil {
		return AddressList{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return AddressList{}, errors.New("address list name is required")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "manual"
	}
	notes, err = validation.TrimField(notes, "notes")
	if err != nil {
		return AddressList{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AddressList{}, err
	}
	defer tx.Rollback()
	storedFields, err := readAddressFields(ctx, tx, []int64{id})
	if err != nil {
		return AddressList{}, err
	}
	definitions, err = mergeAddressFieldDefinitions(storedFields[id], incomingDefinitions)
	if err != nil {
		return AddressList{}, err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE address_lists
		SET name = ?, source = ?, notes = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, name, source, notes, id)
	if err != nil {
		return AddressList{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return AddressList{}, err
	}
	if affected == 0 {
		return AddressList{}, sql.ErrNoRows
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM address_list_entries WHERE address_list_id = ?`,
		id,
	); err != nil {
		return AddressList{}, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM address_list_fields WHERE address_list_id = ?`,
		id,
	); err != nil {
		return AddressList{}, err
	}
	if err := insertAddressFields(ctx, tx, id, definitions); err != nil {
		return AddressList{}, err
	}
	if err := insertEntries(ctx, tx, id, definitions, entries); err != nil {
		return AddressList{}, err
	}
	if err := tx.Commit(); err != nil {
		return AddressList{}, err
	}
	return s.GetAddressList(ctx, id)
}

func (s *Store) DeleteAddressList(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inUse bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM campaigns WHERE address_list_id = ?)`,
		id,
	).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return ErrAddressListInUse
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM address_lists WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

func (s *Store) ListAddressLists(ctx context.Context) ([]AddressList, error) {
	rows, err := s.db.QueryContext(ctx, `
				SELECT l.id, l.name, l.source, l.notes, COUNT(e.id) AS count, l.created_at, l.updated_at
				FROM address_lists l
			LEFT JOIN address_list_entries e ON e.address_list_id = l.id
		GROUP BY l.id
		ORDER BY l.updated_at DESC, l.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lists []AddressList
	for rows.Next() {
		var list AddressList
		if err := rows.Scan(
			&list.ID,
			&list.Name,
			&list.Source,
			&list.Notes,
			&list.Count,
			&list.CreatedAt,
			&list.UpdatedAt,
		); err != nil {
			return nil, err
		}
		lists = append(lists, list)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	listIDs := make([]int64, len(lists))
	for index := range lists {
		listIDs[index] = lists[index].ID
	}
	fieldsByList, err := readAddressFields(ctx, s.db, listIDs)
	if err != nil {
		return nil, err
	}
	for index := range lists {
		lists[index].Fields = fieldsByList[lists[index].ID]
	}
	return lists, nil
}

func (s *Store) GetAddressList(ctx context.Context, id int64) (AddressList, error) {
	var list AddressList
	err := s.db.QueryRowContext(ctx, `
				SELECT l.id, l.name, l.source, l.notes, COUNT(e.id) AS count, l.created_at, l.updated_at
				FROM address_lists l
		LEFT JOIN address_list_entries e ON e.address_list_id = l.id
		WHERE l.id = ?
		GROUP BY l.id
	`, id).Scan(&list.ID, &list.Name, &list.Source, &list.Notes, &list.Count, &list.CreatedAt, &list.UpdatedAt)
	if err != nil {
		return AddressList{}, err
	}
	fieldsByList, err := readAddressFields(ctx, s.db, []int64{id})
	if err != nil {
		return AddressList{}, err
	}
	list.Fields = fieldsByList[id]
	list.Entries, err = s.listAddressEntries(ctx, id, list.Fields)
	if err != nil {
		return AddressList{}, err
	}
	return list, nil
}

func (s *Store) listAddressEntries(
	ctx context.Context,
	listID int64,
	definitions []AddressFieldDefinition,
) ([]AddressEntry, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, email, fields_json
		FROM address_list_entries
		WHERE address_list_id = ?
		ORDER BY id ASC
	`, listID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []AddressEntry
	for rows.Next() {
		var entry AddressEntry
		var fieldsJSON string
		if err := rows.Scan(&entry.ID, &entry.Email, &fieldsJSON); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(fieldsJSON), &entry.Fields); err != nil {
			return nil, err
		}
		entry, err = normalizeAddressEntry(entry, definitions)
		if err != nil {
			return nil, fmt.Errorf("normalize address entry %d: %w", entry.ID, err)
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

type txExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type rowQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readAddressFields(
	ctx context.Context,
	querier rowQuerier,
	listIDs []int64,
) (map[int64][]AddressFieldDefinition, error) {
	fieldsByList := make(map[int64][]AddressFieldDefinition, len(listIDs))
	if len(listIDs) == 0 {
		return fieldsByList, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(listIDs)), ",")
	arguments := make([]any, len(listIDs))
	for index, listID := range listIDs {
		arguments[index] = listID
	}
	query := fmt.Sprintf(`
		SELECT address_list_id, key, label, COALESCE(role, ''), position
		FROM address_list_fields
		WHERE address_list_id IN (%s)
		ORDER BY address_list_id ASC, position ASC, key ASC
	`, placeholders)
	rows, err := querier.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var listID int64
		var definition AddressFieldDefinition
		if err := rows.Scan(
			&listID,
			&definition.Key,
			&definition.Label,
			&definition.Role,
			&definition.Position,
		); err != nil {
			return nil, err
		}
		fieldsByList[listID] = append(fieldsByList[listID], definition)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, listID := range listIDs {
		definitions, err := normalizeAddressFieldDefinitions(fieldsByList[listID])
		if err != nil {
			return nil, fmt.Errorf("address list %d fields: %w", listID, err)
		}
		fieldsByList[listID] = definitions
	}
	return fieldsByList, nil
}

func normalizeAddressFieldDefinitions(
	definitions []AddressFieldDefinition,
) ([]AddressFieldDefinition, error) {
	if len(definitions) > MaxAddressListFields {
		return nil, fmt.Errorf("address lists support at most %d fields", MaxAddressListFields)
	}
	standardFields := DefaultAddressFields()
	standardByKey := make(map[string]AddressFieldDefinition, len(standardFields))
	for _, definition := range standardFields {
		standardByKey[definition.Key] = definition
	}
	seen := make(map[string]bool, len(definitions))
	customFields := make([]AddressFieldDefinition, 0, len(definitions))
	for _, definition := range definitions {
		key, err := validation.NormalizePlaceholderKey(definition.Key)
		if err != nil {
			return nil, err
		}
		if key == "full_name" {
			return nil, errors.New("full_name is a derived placeholder and cannot be an address field")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate address field %q", key)
		}
		seen[key] = true
		label, err := validation.TrimField(
			norm.NFC.String(definition.Label),
			"address field label",
		)
		if err != nil {
			return nil, err
		}
		if label == "" {
			return nil, fmt.Errorf("address field %q label is required", key)
		}
		if standard, ok := standardByKey[key]; ok {
			if definition.Role != standard.Role || label != standard.Label {
				return nil, fmt.Errorf("address field %q must use its standard label and role", key)
			}
			continue
		}
		if definition.Role != AddressFieldRoleNone {
			return nil, fmt.Errorf("custom address field %q cannot use role %q", key, definition.Role)
		}
		customFields = append(customFields, AddressFieldDefinition{
			Key:   key,
			Label: label,
			Role:  AddressFieldRoleNone,
		})
	}
	for _, standard := range standardFields {
		if !seen[standard.Key] {
			return nil, fmt.Errorf("standard address field %q is required", standard.Key)
		}
	}
	normalized := append([]AddressFieldDefinition(nil), standardFields...)
	for _, definition := range customFields {
		definition.Position = len(normalized)
		normalized = append(normalized, definition)
	}
	return normalized, nil
}

func mergeAddressFieldDefinitions(
	stored []AddressFieldDefinition,
	incoming []AddressFieldDefinition,
) ([]AddressFieldDefinition, error) {
	stored, err := normalizeAddressFieldDefinitions(stored)
	if err != nil {
		return nil, err
	}
	incoming, err = normalizeAddressFieldDefinitions(incoming)
	if err != nil {
		return nil, err
	}
	definitions := append([]AddressFieldDefinition(nil), stored...)
	storedByKey := make(map[string]AddressFieldDefinition, len(stored))
	for _, definition := range stored {
		storedByKey[definition.Key] = definition
	}
	for _, definition := range incoming[len(DefaultAddressFields()):] {
		if current, ok := storedByKey[definition.Key]; ok {
			if current.Label != definition.Label || current.Role != definition.Role {
				return nil, fmt.Errorf("saved address field %q cannot be redefined during import", definition.Key)
			}
			continue
		}
		definitions = append(definitions, definition)
	}
	return normalizeAddressFieldDefinitions(definitions)
}

func insertAddressFields(ctx context.Context, tx txExecutor, listID int64, definitions []AddressFieldDefinition) error {
	for _, definition := range definitions {
		var role any
		if definition.Role != AddressFieldRoleNone {
			role = string(definition.Role)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO address_list_fields (address_list_id, key, label, role, position)
			VALUES (?, ?, ?, ?, ?)
		`, listID, definition.Key, definition.Label, role, definition.Position); err != nil {
			return fmt.Errorf("save address field %q: %w", definition.Key, err)
		}
	}
	return nil
}

func insertEntries(
	ctx context.Context,
	tx txExecutor,
	listID int64,
	definitions []AddressFieldDefinition,
	entries []AddressEntry,
) error {
	seen := map[string]bool{}
	for index, entry := range entries {
		entry, err := normalizeAddressEntry(entry, definitions)
		if err != nil {
			return fmt.Errorf("address entry %d: %w", index+1, err)
		}
		if seen[entry.Email] {
			return fmt.Errorf("address entry %d: duplicate email %q", index+1, entry.Email)
		}
		seen[entry.Email] = true
		fieldsJSON, err := json.Marshal(entry.Fields)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO address_list_entries (address_list_id, email, fields_json)
			VALUES (?, ?, ?)
		`, listID, entry.Email, string(fieldsJSON)); err != nil {
			return fmt.Errorf("address entry %d: %w", index+1, err)
		}
	}
	return nil
}

func normalizeAddressEntry(
	entry AddressEntry,
	definitions []AddressFieldDefinition,
) (AddressEntry, error) {
	email, err := validation.NormalizeEmail(entry.Email)
	if err != nil {
		return AddressEntry{}, err
	}
	fields, err := normalizeAddressFields(entry.Fields, email, definitions)
	if err != nil {
		return AddressEntry{}, err
	}
	entry.Email = email
	entry.Fields = fields

	titleCaser := cases.Title(language.Und)
	nameParts := make([]string, 0, 2)
	for _, role := range []AddressFieldRole{
		AddressFieldRoleFirstName,
		AddressFieldRoleLastName,
	} {
		value := strings.Join(strings.Fields(entry.Fields[string(role)]), " ")
		value = norm.NFC.String(value)
		if value != "" {
			nameParts = append(nameParts, titleCaser.String(strings.ToLower(value)))
		}
	}
	entry.DisplayName = strings.Join(nameParts, " ")
	if entry.DisplayName == "" {
		entry.DisplayName = entry.Email
	}
	return entry, nil
}

func normalizeAddressFields(
	fields AddressFields,
	email string,
	definitions []AddressFieldDefinition,
) (AddressFields, error) {
	allowed := make(map[string]bool, len(definitions))
	next := make(AddressFields, len(definitions))
	for _, definition := range definitions {
		allowed[definition.Key] = true
		next[definition.Key] = ""
	}
	seen := map[string]string{}
	for key, value := range fields {
		rawKey := key
		key, err := validation.NormalizePlaceholderKey(key)
		if err != nil {
			return nil, err
		}
		if previous, exists := seen[key]; exists && previous != rawKey {
			return nil, fmt.Errorf("field keys %q and %q normalize to the same placeholder %q", previous, rawKey, key)
		}
		seen[key] = rawKey
		if !allowed[key] {
			return nil, fmt.Errorf("field %q is not defined for this address list", key)
		}
		trimmed, err := validation.TrimField(value, key)
		if err != nil {
			return nil, err
		}
		next[key] = trimmed
	}
	next[string(AddressFieldRoleEmail)] = email
	return next, nil
}

// Campaigns, tasks, and events.

// Durable task queue.

func (s *Store) EnqueueTask(
	ctx context.Context,
	campaignID int64,
	total int,
	metadata taskpkg.Metadata,
	storageKey string,
	profileID int64,
	maxQueued int,
) (taskpkg.Task, error) {
	if campaignID <= 0 {
		return taskpkg.Task{}, errors.New("saved campaign is required")
	}
	if maxQueued < 1 {
		return taskpkg.Task{}, errors.New("maximum queued tasks must be at least one")
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return taskpkg.Task{}, fmt.Errorf("encode task metadata: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return taskpkg.Task{}, err
	}
	defer tx.Rollback()

	var queued int
	if err := tx.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM tasks WHERE status = 'queued'`,
	).Scan(&queued); err != nil {
		return taskpkg.Task{}, err
	}
	if queued >= maxQueued {
		return taskpkg.Task{}, taskpkg.ErrQueueFull
	}
	taskResult, err := tx.ExecContext(
		ctx,
		`INSERT INTO tasks (campaign_id, status, metadata_json, total) VALUES (?, 'queued', ?, ?)`,
		campaignID,
		string(metadataJSON),
		total,
	)
	if err != nil {
		return taskpkg.Task{}, err
	}
	taskID, err := taskResult.LastInsertId()
	if err != nil {
		return taskpkg.Task{}, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO task_inputs (task_id, profile_id, storage_key) VALUES (?, ?, ?)`,
		taskID,
		nullableID(profileID),
		storageKey,
	); err != nil {
		return taskpkg.Task{}, err
	}
	task, err := getTask(ctx, tx, taskID)
	if err != nil {
		return taskpkg.Task{}, err
	}
	if err := tx.Commit(); err != nil {
		return taskpkg.Task{}, err
	}
	return task, nil
}

func (s *Store) ClaimNextTask(ctx context.Context) (int64, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var taskID int64
	err = tx.QueryRowContext(ctx, `
		SELECT t.id
		FROM tasks t
		JOIN task_inputs i ON i.task_id = t.id
		WHERE t.status = 'queued'
		ORDER BY t.id ASC
		LIMIT 1
	`).Scan(&taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE tasks
		SET status = 'preparing', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND status = 'queued'
	`, taskID)
	if err != nil {
		return 0, false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if count != 1 {
		return 0, false, nil
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return taskID, true, nil
}

func (s *Store) GetTaskInput(ctx context.Context, taskID int64) (taskpkg.Input, error) {
	var input taskpkg.Input
	err := s.db.QueryRowContext(
		ctx,
		`SELECT task_id, COALESCE(profile_id, 0), storage_key FROM task_inputs WHERE task_id = ?`,
		taskID,
	).Scan(&input.TaskID, &input.ProfileID, &input.StorageKey)
	return input, err
}

func (s *Store) GetQueuedTaskInput(ctx context.Context, taskID int64) (taskpkg.Input, error) {
	var input taskpkg.Input
	err := s.db.QueryRowContext(ctx, `
		SELECT i.task_id, COALESCE(i.profile_id, 0), i.storage_key
		FROM task_inputs i
		JOIN tasks t ON t.id = i.task_id
		WHERE i.task_id = ? AND t.status = 'queued'
	`, taskID).Scan(&input.TaskID, &input.ProfileID, &input.StorageKey)
	return input, err
}

func (s *Store) TakeTaskInput(ctx context.Context, taskID int64) (taskpkg.Input, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return taskpkg.Input{}, err
	}
	defer tx.Rollback()
	var input taskpkg.Input
	if err := tx.QueryRowContext(
		ctx,
		`SELECT task_id, COALESCE(profile_id, 0), storage_key FROM task_inputs WHERE task_id = ?`,
		taskID,
	).Scan(&input.TaskID, &input.ProfileID, &input.StorageKey); err != nil {
		return taskpkg.Input{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_inputs WHERE task_id = ?`, taskID); err != nil {
		return taskpkg.Input{}, err
	}
	if err := tx.Commit(); err != nil {
		return taskpkg.Input{}, err
	}
	return input, nil
}

func (s *Store) ListInterruptedTaskIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM tasks
		WHERE status IN ('preparing', 'running')
		   OR (
		       status = 'queued'
		       AND NOT EXISTS (SELECT 1 FROM task_inputs i WHERE i.task_id = tasks.id)
		   )
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var taskIDs []int64
	for rows.Next() {
		var taskID int64
		if err := rows.Scan(&taskID); err != nil {
			return nil, err
		}
		taskIDs = append(taskIDs, taskID)
	}
	return taskIDs, rows.Err()
}

func (s *Store) ReconcileTaskInputs(ctx context.Context) ([]string, []string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT i.storage_key, t.status
		FROM task_inputs i
		JOIN tasks t ON t.id = i.task_id
	`)
	if err != nil {
		return nil, nil, err
	}
	var retained, stale []string
	for rows.Next() {
		var key, status string
		if err := rows.Scan(&key, &status); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		if status == "queued" {
			retained = append(retained, key)
		} else {
			stale = append(stale, key)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM task_inputs
		WHERE task_id IN (SELECT id FROM tasks WHERE status <> 'queued')
	`); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return retained, stale, nil
}

// Campaigns and task history.

func (s *Store) SaveCampaign(ctx context.Context, campaign Campaign) (Campaign, error) {
	campaign.Name = strings.TrimSpace(campaign.Name)
	if campaign.Name == "" {
		return Campaign{}, errors.New("campaign name is required")
	}
	if campaign.ID != NewCampaignID && campaign.ID <= 0 {
		return Campaign{}, errors.New("campaign id must be -1 or positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Campaign{}, err
	}
	defer tx.Rollback()
	if campaign.ID == NewCampaignID {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO campaigns (
				name, address_list_id, profile_id, subject, body, html_body,
				request_delivery_notice, remove_diacritics,
				first_name_format, last_name_format, full_name_format
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, campaignValues(campaign)...)
		if err != nil {
			return Campaign{}, err
		}
		campaign.ID, err = result.LastInsertId()
		if err != nil {
			return Campaign{}, err
		}
	} else {
		values := append(campaignValues(campaign), campaign.ID)
		result, err := tx.ExecContext(ctx, `
			UPDATE campaigns SET
				name = ?, address_list_id = ?, profile_id = ?, subject = ?, body = ?, html_body = ?,
				request_delivery_notice = ?, remove_diacritics = ?,
				first_name_format = ?, last_name_format = ?, full_name_format = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ?
		`, values...)
		if err != nil {
			return Campaign{}, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return Campaign{}, err
		}
		if count == 0 {
			return Campaign{}, sql.ErrNoRows
		}
		if _, err := tx.ExecContext(
			ctx,
			`DELETE FROM campaign_attachments WHERE campaign_id = ?`,
			campaign.ID,
		); err != nil {
			return Campaign{}, err
		}
	}
	for position, attachment := range campaign.Message.Attachments {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO campaign_attachments (
				campaign_id, position, filename, output_filename, content
			) VALUES (?, ?, ?, ?, ?)
		`, campaign.ID, position, attachment.Filename, attachment.OutputFilename, attachment.Content); err != nil {
			return Campaign{}, err
		}
	}
	saved, err := getCampaign(ctx, tx, campaign.ID, true)
	if err != nil {
		return Campaign{}, err
	}
	if err := tx.Commit(); err != nil {
		return Campaign{}, err
	}
	return saved, nil
}

func campaignValues(campaign Campaign) []any {
	return []any{
		campaign.Name,
		nullableID(campaign.AddressListID),
		campaign.ProfileID,
		campaign.Message.Subject,
		campaign.Message.Body,
		campaign.Message.HTMLBody,
		campaign.Message.RequestDeliveryNotice,
		campaign.Personalization.RemoveDiacritics,
		campaign.Personalization.FirstNameFormat,
		campaign.Personalization.LastNameFormat,
		campaign.Personalization.FullNameFormat,
	}
}

func (s *Store) ListCampaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := s.db.QueryContext(ctx, campaignSelect+` ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	var campaigns []Campaign
	for rows.Next() {
		campaign, err := scanCampaign(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		campaigns = append(campaigns, campaign)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range campaigns {
		campaigns[index].Message.Attachments, err = listCampaignAttachments(
			ctx,
			s.db,
			campaigns[index].ID,
			false,
		)
		if err != nil {
			return nil, err
		}
	}
	return campaigns, nil
}

func (s *Store) GetCampaign(ctx context.Context, id int64) (Campaign, error) {
	return getCampaign(ctx, s.db, id, true)
}

func (s *Store) DeleteCampaign(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var inUse bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tasks
			WHERE campaign_id = ?
			  AND status NOT IN ('completed', 'completed_with_errors', 'cancelled', 'interrupted')
		)
	`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return ErrCampaignInUse
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM campaigns WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

const campaignSelect = `
	SELECT id, name, COALESCE(address_list_id, 0), profile_id,
	       subject, body, html_body, request_delivery_notice, remove_diacritics,
	       first_name_format, last_name_format, full_name_format, created_at, updated_at
	FROM campaigns`

type campaignQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type campaignScanner interface {
	Scan(...any) error
}

func getCampaign(ctx context.Context, queryer campaignQueryer, id int64, includeContent bool) (Campaign, error) {
	campaign, err := scanCampaign(queryer.QueryRowContext(ctx, campaignSelect+` WHERE id = ?`, id))
	if err != nil {
		return Campaign{}, err
	}
	campaign.Message.Attachments, err = listCampaignAttachments(ctx, queryer, id, includeContent)
	return campaign, err
}

func scanCampaign(scanner campaignScanner) (Campaign, error) {
	var campaign Campaign
	err := scanner.Scan(
		&campaign.ID,
		&campaign.Name,
		&campaign.AddressListID,
		&campaign.ProfileID,
		&campaign.Message.Subject,
		&campaign.Message.Body,
		&campaign.Message.HTMLBody,
		&campaign.Message.RequestDeliveryNotice,
		&campaign.Personalization.RemoveDiacritics,
		&campaign.Personalization.FirstNameFormat,
		&campaign.Personalization.LastNameFormat,
		&campaign.Personalization.FullNameFormat,
		&campaign.CreatedAt,
		&campaign.UpdatedAt,
	)
	return campaign, err
}

func listCampaignAttachments(
	ctx context.Context,
	queryer campaignQueryer,
	campaignID int64,
	includeContent bool,
) ([]mail.Attachment, error) {
	contentColumn := "NULL"
	if includeContent {
		contentColumn = "content"
	}
	rows, err := queryer.QueryContext(ctx, `
		SELECT filename, output_filename, LENGTH(content), `+contentColumn+`
		FROM campaign_attachments
		WHERE campaign_id = ?
		ORDER BY position
	`, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attachments := []mail.Attachment{}
	for rows.Next() {
		var attachment mail.Attachment
		if err := rows.Scan(
			&attachment.Filename,
			&attachment.OutputFilename,
			&attachment.Size,
			&attachment.Content,
		); err != nil {
			return nil, err
		}
		attachments = append(attachments, attachment)
	}
	return attachments, rows.Err()
}

func (s *Store) ListTasks(ctx context.Context) ([]taskpkg.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, campaign_id, metadata_json, status, total, sent, failed,
		       skipped, last_error, created_at, updated_at
		FROM tasks
		ORDER BY id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []taskpkg.Task
	for rows.Next() {
		var task taskpkg.Task
		var metadataJSON string
		if err := rows.Scan(
			&task.ID,
			&task.CampaignID,
			&metadataJSON,
			&task.Status,
			&task.Total,
			&task.Sent,
			&task.Failed,
			&task.Skipped,
			&task.LastError,
			&task.CreatedAt,
			&task.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if err := decodeTaskMetadata(&task, metadataJSON); err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *Store) UpdateTaskStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tasks SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		status,
		id,
	)
	return err
}

func (s *Store) FinishTask(ctx context.Context, id int64, status, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE tasks
		SET status = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, lastError, id)
	return err
}

func (s *Store) IncrementTaskSent(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tasks SET sent = sent + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	return err
}

func (s *Store) IncrementTaskFailed(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tasks SET failed = failed + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	return err
}

func (s *Store) IncrementTaskSkipped(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE tasks SET skipped = skipped + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		id,
	)
	return err
}

func (s *Store) GetTask(ctx context.Context, id int64) (taskpkg.Task, error) {
	return getTask(ctx, s.db, id)
}

type taskQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func getTask(ctx context.Context, queryer taskQueryer, id int64) (taskpkg.Task, error) {
	var task taskpkg.Task
	var metadataJSON string
	err := queryer.QueryRowContext(ctx, `
		SELECT id, campaign_id, metadata_json, status, total, sent,
		       failed, skipped, last_error, created_at, updated_at
		FROM tasks
		WHERE id = ?
	`, id).Scan(
		&task.ID,
		&task.CampaignID,
		&metadataJSON,
		&task.Status,
		&task.Total,
		&task.Sent,
		&task.Failed,
		&task.Skipped,
		&task.LastError,
		&task.CreatedAt,
		&task.UpdatedAt,
	)
	if err != nil {
		return taskpkg.Task{}, err
	}
	if err := decodeTaskMetadata(&task, metadataJSON); err != nil {
		return taskpkg.Task{}, err
	}
	return task, nil
}

func decodeTaskMetadata(task *taskpkg.Task, metadataJSON string) error {
	if err := json.Unmarshal([]byte(metadataJSON), &task.Metadata); err != nil {
		return fmt.Errorf("decode task metadata: %w", err)
	}
	task.CampaignName = task.Metadata.CampaignName
	return nil
}

// Delivery outcomes.

func (s *Store) CreateDelivery(ctx context.Context, d MessageDelivery) (MessageDelivery, error) {
	email, err := validation.NormalizeEmail(d.Email)
	if err != nil {
		return MessageDelivery{}, err
	}
	if d.Attempt <= 0 {
		d.Attempt = 1
	}
	if d.Status == "" {
		d.Status = "attempted"
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO message_deliveries
			(task_id, campaign_id, address_entry_id, email, status, attempt, provider_message_id, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		d.TaskID,
		d.CampaignID,
		d.AddressEntryID,
		email,
		d.Status,
		d.Attempt,
		d.ProviderMessageID,
		d.LastError,
	)
	if err != nil {
		return MessageDelivery{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return MessageDelivery{}, err
	}
	return s.GetDelivery(ctx, id)
}

func (s *Store) UpdateDeliveryStatus(ctx context.Context, id int64, status, providerMessageID, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE message_deliveries
		SET status = ?, provider_message_id = ?, last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, providerMessageID, lastError, id)
	return err
}

func (s *Store) UpdateDeliveryAttempt(ctx context.Context, id int64, attempt int, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE message_deliveries
		SET attempt = ?, status = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, attempt, status, id)
	return err
}

func (s *Store) GetDelivery(ctx context.Context, id int64) (MessageDelivery, error) {
	var d MessageDelivery
	err := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, campaign_id, address_entry_id, email, status, attempt,
		       provider_message_id, last_error, created_at, updated_at
		FROM message_deliveries
		WHERE id = ?
	`, id).Scan(
		&d.ID,
		&d.TaskID,
		&d.CampaignID,
		&d.AddressEntryID,
		&d.Email,
		&d.Status,
		&d.Attempt,
		&d.ProviderMessageID,
		&d.LastError,
		&d.CreatedAt,
		&d.UpdatedAt,
	)
	return d, err
}

func (s *Store) ListDeliveriesForTask(ctx context.Context, taskID int64) ([]MessageDelivery, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, campaign_id, address_entry_id, email, status, attempt,
		       provider_message_id, last_error, created_at, updated_at
		FROM message_deliveries
		WHERE task_id = ?
		ORDER BY id ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deliveries []MessageDelivery
	for rows.Next() {
		var d MessageDelivery
		if err := rows.Scan(
			&d.ID,
			&d.TaskID,
			&d.CampaignID,
			&d.AddressEntryID,
			&d.Email,
			&d.Status,
			&d.Attempt,
			&d.ProviderMessageID,
			&d.LastError,
			&d.CreatedAt,
			&d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		deliveries = append(deliveries, d)
	}
	return emptyIfNil(deliveries), rows.Err()
}

// Suppressions.

func (s *Store) IsSuppressed(ctx context.Context, email string) (bool, error) {
	normalized, err := validation.NormalizeEmail(email)
	if err != nil {
		return false, err
	}
	var exists int
	err = s.db.QueryRowContext(ctx, `SELECT 1 FROM suppressions WHERE email = ? LIMIT 1`, normalized).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) AddSuppression(ctx context.Context, email, reason string) error {
	normalized, err := validation.NormalizeEmail(email)
	if err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "manual"
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO suppressions (email, reason) VALUES (?, ?)
		ON CONFLICT(email) DO UPDATE SET reason = excluded.reason
	`, normalized, reason)
	return err
}

func (s *Store) ListSuppressions(ctx context.Context) ([]Suppression, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, email, reason, created_at FROM suppressions ORDER BY email ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var suppressions []Suppression
	for rows.Next() {
		var suppression Suppression
		if err := rows.Scan(&suppression.ID, &suppression.Email, &suppression.Reason, &suppression.CreatedAt); err != nil {
			return nil, err
		}
		suppressions = append(suppressions, suppression)
	}
	return emptyIfNil(suppressions), rows.Err()
}

func (s *Store) DeleteSuppression(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM suppressions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Task interruption and queued cancellation.

func (s *Store) FinalizeInterruptedTask(
	ctx context.Context,
	taskID int64,
	emails []string,
	diagnostic string,
) error {
	normalizedEmails, err := normalizeTaskEmails(emails)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	campaignID, err := taskCampaignID(ctx, tx, taskID, "")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE message_deliveries
		SET status = 'interrupted', last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE task_id = ? AND status IN ('attempted', 'retrying')
	`, diagnostic, taskID); err != nil {
		return err
	}
	if err := insertMissingTaskOutcomes(
		ctx,
		tx,
		taskID,
		campaignID,
		normalizedEmails,
		"interrupted",
		diagnostic,
	); err != nil {
		return err
	}
	if err := finalizeTaskStatus(ctx, tx, taskID, "interrupted", diagnostic); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelQueuedCampaignTask(
	ctx context.Context,
	taskID int64,
	emails []string,
) (bool, error) {
	normalizedEmails, err := normalizeTaskEmails(emails)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	campaignID, err := taskCampaignID(ctx, tx, taskID, "queued")
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	const diagnostic = "task cancelled by operator"
	if err := insertMissingTaskOutcomes(
		ctx,
		tx,
		taskID,
		campaignID,
		normalizedEmails,
		"cancelled",
		diagnostic,
	); err != nil {
		return false, err
	}
	if err := finalizeTaskStatus(ctx, tx, taskID, "cancelled", diagnostic); err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM task_inputs WHERE task_id = ?`, taskID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func taskCampaignID(
	ctx context.Context,
	tx *sql.Tx,
	taskID int64,
	requiredStatus string,
) (sql.NullInt64, error) {
	query := `SELECT campaign_id FROM tasks WHERE id = ?`
	arguments := []any{taskID}
	if requiredStatus != "" {
		query += ` AND status = ?`
		arguments = append(arguments, requiredStatus)
	}
	var campaignID sql.NullInt64
	err := tx.QueryRowContext(ctx, query, arguments...).Scan(&campaignID)
	return campaignID, err
}

func insertMissingTaskOutcomes(
	ctx context.Context,
	tx *sql.Tx,
	taskID int64,
	campaignID sql.NullInt64,
	emails []string,
	status string,
	diagnostic string,
) error {
	for _, email := range emails {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO message_deliveries (
				task_id, campaign_id, address_entry_id, email, status, attempt, last_error
			)
			SELECT ?, ?, NULL, ?, ?, 1, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM message_deliveries WHERE task_id = ? AND email = ?
			)
		`, taskID, campaignID, email, status, diagnostic, taskID, email); err != nil {
			return err
		}
	}
	return nil
}

func finalizeTaskStatus(
	ctx context.Context,
	tx *sql.Tx,
	taskID int64,
	status string,
	diagnostic string,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE tasks SET
			status = ?,
			last_error = ?,
			sent = (
				SELECT COUNT(*) FROM message_deliveries
				WHERE task_id = ? AND status IN ('sent', 'generated')
			),
			failed = (
				SELECT COUNT(*) FROM message_deliveries
				WHERE task_id = ? AND status LIKE 'failed_%'
			),
			skipped = (
				SELECT COUNT(*) FROM message_deliveries
				WHERE task_id = ? AND status IN (
					'skipped_suppressed', 'cancelled', 'interrupted',
					'not_attempted_configuration'
				)
			),
			updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, status, diagnostic, taskID, taskID, taskID, taskID)
	return err
}

func normalizeTaskEmails(emails []string) ([]string, error) {
	normalized := make([]string, 0, len(emails))
	seen := make(map[string]struct{}, len(emails))
	for _, email := range emails {
		value, err := validation.NormalizeEmail(email)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

// Shared helpers.

func emptyIfNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
