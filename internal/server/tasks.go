package server

import (
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/dvoulgaridis/bulk-mail/internal/store"
	taskpkg "github.com/dvoulgaridis/bulk-mail/internal/tasks"
)

// Task HTTP responses.

type failureGroup struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type taskReport struct {
	Task             taskpkg.Task            `json:"task"`
	Metadata         taskpkg.Metadata        `json:"metadata"`
	Deliveries       []store.MessageDelivery `json:"deliveries"`
	StatusCounts     map[string]int          `json:"statusCounts"`
	Failures         []failureGroup          `json:"failures"`
	ArchiveAvailable bool                    `json:"archiveAvailable"`
}

// Task routing.

func (s *Server) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/tasks/"), "/")
	parts := strings.Split(path, "/")
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	action := "report"
	if len(parts) > 1 {
		action = parts[1]
	}
	switch action {
	case "report":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.writeTaskReport(w, r, id)
	case "cancel":
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		cancelled, err := s.campaignService.CancelTask(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "cancel task failed")
			return
		}
		if !cancelled {
			writeError(w, http.StatusConflict, "task is not active")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]bool{"cancelled": true})
	case "archive":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.writeGeneratedArchive(w, r, id)
	default:
		http.NotFound(w, r)
	}
}

// Task reports.

func (s *Server) writeTaskReport(w http.ResponseWriter, r *http.Request, id int64) {
	task, err := s.repo.GetTask(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	deliveries, err := s.repo.ListDeliveriesForTask(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	counts := map[string]int{}
	failureCounts := map[string]int{}
	for _, delivery := range deliveries {
		counts[delivery.Status]++
		if strings.HasPrefix(delivery.Status, "failed_") {
			failureCounts[strings.TrimPrefix(delivery.Status, "failed_")]++
		}
	}
	categories := []string{"configuration", "permanent", "transient", "processing"}
	groups := make([]failureGroup, 0, len(failureCounts))
	for _, category := range categories {
		if failureCounts[category] > 0 {
			groups = append(groups, failureGroup{Category: category, Count: failureCounts[category]})
		}
	}
	writeJSON(w, http.StatusOK, taskReport{
		Task:             task,
		Metadata:         task.Metadata,
		Deliveries:       deliveries,
		StatusCounts:     counts,
		Failures:         groups,
		ArchiveAvailable: s.campaignService.ArchiveAvailable(id),
	})
}

// Generated archives.

func (s *Server) writeGeneratedArchive(w http.ResponseWriter, r *http.Request, id int64) {
	archiveResult, err := s.campaignService.TakeGeneratedArchive(r.Context(), id)
	if err != nil {
		writeAppError(w, err)
		return
	}
	defer archiveResult.Cleanup()
	archive, err := os.Open(archiveResult.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "open generated archive failed")
		return
	}
	defer archive.Close()
	info, err := archive.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "inspect generated archive failed")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType(
		"attachment",
		map[string]string{"filename": archiveResult.Filename},
	))
	http.ServeContent(w, r, archiveResult.Filename, info.ModTime(), archive)
}
