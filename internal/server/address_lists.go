package server

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/store"
)

type addressListRequest struct {
	Name   string                         `json:"name"`
	Source string                         `json:"source"`
	Notes  string                         `json:"notes"`
	Fields []store.AddressFieldDefinition `json:"fields"`
}

func (s *Server) handleAddressLists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.saveAddressList(w, r, 0)
}

func (s *Server) saveAddressList(w http.ResponseWriter, r *http.Request, id int64) {
	var body addressListRequest
	if !readJSON(w, r, &body) {
		return
	}
	list, err := s.repo.SaveAddressList(r.Context(), store.AddressList{
		ID: id, Name: body.Name, Source: body.Source, Notes: body.Notes,
		Fields: body.Fields,
	})
	if err != nil {
		writeAddressError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": list.ID})
}

func (s *Server) handleAddressListByID(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/address-lists/"), "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid address list ID")
		return
	}
	if len(parts) > 1 {
		s.handleAddressEntries(w, r, id, strings.Join(parts[1:], "/"))
		return
	}
	switch r.Method {
	case http.MethodGet:
		list, err := s.repo.GetAddressList(r.Context(), id)
		if err != nil {
			writeAddressError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	case http.MethodPut:
		s.saveAddressList(w, r, id)
	case http.MethodDelete:
		if err := s.repo.DeleteAddressList(r.Context(), id); err != nil {
			writeAddressError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAddressEntries(w http.ResponseWriter, r *http.Request, id int64, path string) {
	operation, allowed := "", "POST"
	switch path {
	case "entries":
		allowed = "POST, PATCH"
		if r.Method == http.MethodPost {
			operation = "insert"
		}
		if r.Method == http.MethodPatch {
			operation = "update"
		}
	case "entries/import":
		if r.Method == http.MethodPost {
			operation = "insert"
		}
	case "entries/delete":
		if r.Method == http.MethodPost {
			operation = "delete"
		}
	default:
		http.NotFound(w, r)
		return
	}
	if operation == "" {
		w.Header().Set("Allow", allowed)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var command store.EntryWriteCommand
	if !readJSON(w, r, &command) {
		return
	}
	if path != "entries/import" && len(command.Fields) > 0 {
		writeError(w, http.StatusBadRequest, "field definitions require the import endpoint")
		return
	}
	if (operation == "delete" && (len(command.IDs) == 0 || len(command.Entries) > 0)) ||
		(operation != "delete" && (len(command.Entries) == 0 || len(command.IDs) > 0)) {
		writeError(w, http.StatusBadRequest, "provide a nonempty array of entries or deletion IDs")
		return
	}
	result, err := s.repo.WriteAddressEntries(r.Context(), id, operation, command)
	if err != nil {
		writeAddressError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeAddressError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, store.ErrAddressListInUse) {
		status = http.StatusConflict
	} else if errors.Is(err, sql.ErrNoRows) {
		status = http.StatusNotFound
	}
	writeError(w, status, err.Error())
}
