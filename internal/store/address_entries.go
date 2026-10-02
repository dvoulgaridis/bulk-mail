package store

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/validation"
)

// Entry writes commit independently. Serialize batches so renumbering cannot
// change another in-flight batch's targets between its individual writes.
func (s *Store) WriteAddressEntries(
	ctx context.Context, listID int64, operation string, command EntryWriteCommand,
) (EntryWriteResult, error) {
	s.entryWrites.Lock()
	defer s.entryWrites.Unlock()
	list, err := s.GetAddressList(ctx, listID)
	if err != nil {
		return EntryWriteResult{}, err
	}
	if operation != "insert" && operation != "update" && operation != "delete" {
		return EntryWriteResult{}, errors.New("unknown address operation")
	}
	if len(command.Fields) > 0 {
		if operation != "insert" {
			return EntryWriteResult{}, errors.New("fields require an import")
		}
		list.Fields, err = mergeAddressFieldDefinitions(list.Fields, command.Fields)
		if err != nil {
			return EntryWriteResult{}, err
		}
		_, err = s.SaveAddressList(ctx, list)
		if err != nil {
			return EntryWriteResult{}, err
		}
	}
	count := len(command.Entries)
	if operation == "delete" {
		count = len(command.IDs)
	}
	result := EntryWriteResult{Results: make([]EntryOutcome, count)}
	order := make([]int, count)
	for index := range order {
		order[index] = index
	}
	if operation == "delete" {
		// Highest first: lower requested IDs remain unchanged after compaction.
		slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(command.IDs[b], command.IDs[a]) })
	}
	var deletedID int64
	for _, index := range order {
		entry := AddressEntry{}
		if operation == "delete" {
			entry.ID = command.IDs[index]
		} else {
			entry = command.Entries[index]
		}
		outcome := EntryOutcome{Index: index, ID: entry.ID}
		if result.Stopped != "" {
			outcome.Status, outcome.Message = "not_processed", result.Stopped
		} else if operation == "delete" && entry.ID == deletedID && deletedID > 0 {
			outcome.Status, outcome.Message = "not_found", "address no longer exists"
		} else {
			outcome.ID, outcome.Status, err = s.writeAddressEntry(
				ctx, listID, operation, entry, list.Fields,
			)
			if outcome.Status == "deleted" && err == nil {
				deletedID = entry.ID
			}
			if err != nil {
				if operation == "insert" {
					outcome.ID = 0
				}
				outcome.Message = err.Error()
				if outcome.Status == "duplicate" {
					outcome.Message = "email already exists in this list"
				}
				if outcome.Status == "" {
					outcome.Status = "failed"
					result.Stopped = "Address writes stopped: " + err.Error()
				}
			}
		}
		result.Results[index] = outcome
	}
	return result, nil
}

func (s *Store) writeAddressEntry(
	ctx context.Context, listID int64, operation string,
	entry AddressEntry, definitions []AddressFieldDefinition,
) (id int64, status string, err error) {
	id = entry.ID
	if operation == "insert" {
		id = 0
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return id, "", err
	}
	defer tx.Rollback()
	var previousEmail string
	if operation != "insert" {
		err := tx.QueryRowContext(ctx,
			`SELECT email FROM address_list_entries WHERE id = ? AND address_list_id = ?`,
			id, listID,
		).Scan(&previousEmail)
		if errors.Is(err, sql.ErrNoRows) {
			return entry.ID, "not_found", errors.New("address no longer exists")
		}
		if err != nil {
			return id, "", err
		}
	}
	if operation == "delete" {
		var deleted sql.Result
		deleted, err = tx.ExecContext(ctx,
			`DELETE FROM address_list_entries WHERE id = ? AND address_list_id = ?`, id, listID)
		if err == nil {
			err = requireEntryChange(deleted)
		}
		if err == nil {
			// Negative staging avoids primary-key collisions during compaction.
			_, err = tx.ExecContext(ctx, `
				UPDATE address_list_entries SET id = -id
				WHERE address_list_id = ? AND id > ?`, listID, id)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, `
				UPDATE address_list_entries SET id = -id - 1
				WHERE address_list_id = ? AND id < 0`, listID)
		}
		status = "deleted"
	} else {
		email := entry.Fields["email"]
		if operation == "insert" || email != previousEmail {
			email, err = validation.NormalizeEmail(email)
			if err != nil {
				return id, "invalid", err
			}
		}
		fields, err := normalizeAddressFields(entry.Fields, definitions)
		if err != nil {
			return id, "invalid", err
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return id, "invalid", err
		}
		var duplicate int
		err = tx.QueryRowContext(ctx, `
			SELECT 1 FROM address_list_entries
			WHERE address_list_id = ? AND lower(email) = ? AND id != ?`, listID, email, id).Scan(&duplicate)
		if err == nil {
			return id, "duplicate", errors.New("email already exists in this list")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return id, "", err
		}
		if operation == "insert" {
			if err := tx.QueryRowContext(ctx, `
				SELECT COALESCE(MAX(id), 0) + 1 FROM address_list_entries WHERE address_list_id = ?`,
				listID).Scan(&id); err != nil {
				return id, "", err
			}
			var inserted sql.Result
			inserted, err = tx.ExecContext(ctx, `
				INSERT INTO address_list_entries (address_list_id, id, email, fields_json)
				VALUES (?, ?, ?, ?)`, listID, id, email, string(encoded))
			if err == nil {
				err = requireEntryChange(inserted)
			}
			status = "inserted"
		} else {
			var updated sql.Result
			updated, err = tx.ExecContext(ctx,
				`UPDATE address_list_entries SET email = ?, fields_json = ? WHERE id = ? AND address_list_id = ?`,
				email, string(encoded), id, listID)
			if err == nil {
				err = requireEntryChange(updated)
			}
			status = "updated"
		}
		if err != nil {
			return id, entryErrorStatus(err), err
		}
	}
	if err != nil {
		return id, entryErrorStatus(err), err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE address_lists SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		listID); err != nil {
		return id, "", err
	}
	if err := tx.Commit(); err != nil {
		return id, "", err
	}
	return id, status, nil
}

func requireEntryChange(result sql.Result) error {
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return sql.ErrNoRows
	}
	return err
}

// Constraint failures concern one entry; connection, cancellation and storage
// failures stop the batch instead of repeatedly attempting a broken database.
func entryErrorStatus(err error) string {
	var sqliteError interface{ Code() int }
	if errors.As(err, &sqliteError) {
		if sqliteError.Code() == 2067 {
			return "duplicate"
		}
		if sqliteError.Code()&255 == 19 {
			return "rejected"
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "not_found"
	}
	return ""
}

// Suppressions have stable IDs and require only one statement per entry.
func (s *Store) WriteSuppressions(
	ctx context.Context, emails []string, ids []int64, reason string, remove bool,
) EntryWriteResult {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "manual"
	}
	count := len(emails)
	if remove {
		count = len(ids)
	}
	result := EntryWriteResult{Results: make([]EntryOutcome, 0, count)}
	for index := 0; index < count; index++ {
		outcome := EntryOutcome{Index: index}
		if remove {
			outcome.ID = ids[index]
		}
		if result.Stopped != "" {
			outcome.Status, outcome.Message = "not_processed", result.Stopped
		} else {
			var err error
			if remove {
				err = s.DeleteSuppression(ctx, ids[index])
				outcome.Status = "deleted"
			} else {
				var email string
				email, err = validation.NormalizeEmail(emails[index])
				if err != nil {
					outcome.Status = "invalid"
				} else {
					var inserted sql.Result
					inserted, err = s.db.ExecContext(ctx,
						`INSERT INTO suppressions (email, reason) VALUES (?, ?)`, email, reason)
					if err == nil {
						err = requireEntryChange(inserted)
					}
					if err == nil {
						outcome.ID, err = inserted.LastInsertId()
					}
					outcome.Status = "inserted"
				}
			}
			if err != nil {
				outcome.Message = err.Error()
				if outcome.Status != "invalid" {
					outcome.Status = entryErrorStatus(err)
				}
				if outcome.Status == "duplicate" {
					outcome.Message = "email is already suppressed"
				}
				if outcome.Status == "" {
					outcome.Status = "failed"
					result.Stopped = fmt.Sprintf("Suppression writes stopped: %v", err)
				}
			}
		}
		result.Results = append(result.Results, outcome)
	}
	return result
}
