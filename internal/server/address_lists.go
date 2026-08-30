package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

type addressListRequest struct {
	Name    string                         `json:"name"`
	Source  string                         `json:"source"`
	Notes   string                         `json:"notes"`
	Fields  []store.AddressFieldDefinition `json:"fields"`
	Entries []addressEntryWriteRequest     `json:"entries"`
}

type addressEntryWriteRequest struct {
	Email  string              `json:"email"`
	Fields store.AddressFields `json:"fields"`
}

func (request addressListRequest) addressEntries() []store.AddressEntry {
	entries := make([]store.AddressEntry, len(request.Entries))
	for index, entry := range request.Entries {
		entries[index] = store.AddressEntry{
			Email:  entry.Email,
			Fields: entry.Fields,
		}
	}
	return entries
}

func (s *Server) handleImportAddressList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body addressListRequest
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Entries) == 0 {
		writeError(w, http.StatusBadRequest, "No addresses to import.")
		return
	}
	source := strings.TrimSpace(body.Source)
	if source == "" {
		source = "file"
	}
	list, err := s.repo.CreateAddressList(
		r.Context(),
		body.Name,
		source,
		body.Notes,
		body.Fields,
		body.addressEntries(),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, list)
}

func (s *Server) handleAddressListByID(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r.URL.Path, "/api/address-lists/")
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.repo.GetAddressList(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "address list not found")
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPut:
		var body addressListRequest
		if !readJSON(w, r, &body) {
			return
		}
		list, err := s.repo.ReplaceAddressList(
			r.Context(),
			id,
			body.Name,
			body.Source,
			body.Notes,
			body.Fields,
			body.addressEntries(),
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodDelete:
		if err := s.repo.DeleteAddressList(r.Context(), id); err != nil {
			switch {
			case errors.Is(err, store.ErrAddressListInUse):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, sql.ErrNoRows):
				writeError(w, http.StatusNotFound, "address list not found")
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
