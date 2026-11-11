package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/maintenance"
)

const (
	adminMaintenancePath    = "/admin/maintenance"
	maxMaintenanceBodyBytes = 1024
	maintenanceBodyShape    = `body must be {"in": "30m"} or {"at": "2026-01-02T15:04:05Z"}`
)

type maintenanceRequest struct {
	At string `json:"at"`
	In string `json:"in"`
}

type maintenanceResponse struct {
	MaintenanceAt int64 `json:"maintenance_at,omitempty"`
	ServerTime    int64 `json:"server_time"`
}

func (r maintenanceRequest) moment(now time.Time) (time.Time, error) {
	if (r.At == "") == (r.In == "") {
		return time.Time{}, errors.New(maintenanceBodyShape)
	}
	if r.In != "" {
		wait, err := time.ParseDuration(r.In)
		if err != nil {
			return time.Time{}, errors.New(maintenanceBodyShape)
		}
		return now.Add(wait), nil
	}
	at, err := time.Parse(time.RFC3339, r.At)
	if err != nil {
		return time.Time{}, errors.New(maintenanceBodyShape)
	}
	return at, nil
}

func (s *Server) maintenanceStatus() maintenanceResponse {
	status := maintenanceResponse{ServerTime: s.now().Unix()}
	if at, scheduled := s.maintenance.At(); scheduled {
		status.MaintenanceAt = at.Unix()
	}
	return status
}

func (s *Server) showMaintenance(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.maintenanceStatus())
}

func (s *Server) scheduleMaintenance(w http.ResponseWriter, r *http.Request) {
	var request maintenanceRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxMaintenanceBodyBytes)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, maintenanceBodyShape)
		return
	}
	at, err := request.moment(s.now())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = s.maintenance.Set(at)
	if errors.Is(err, maintenance.ErrNotAhead) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		s.log.Error("schedule maintenance", "error", err)
		writeError(w, http.StatusInternalServerError, "could not schedule maintenance")
		return
	}
	s.log.Info("maintenance scheduled", "at", at.UTC().Format(time.RFC3339))
	writeJSON(w, http.StatusOK, s.maintenanceStatus())
}

func (s *Server) cancelMaintenance(w http.ResponseWriter, _ *http.Request) {
	if err := s.maintenance.Clear(); err != nil {
		s.log.Error("cancel maintenance", "error", err)
		writeError(w, http.StatusInternalServerError, "could not cancel maintenance")
		return
	}
	s.log.Info("maintenance cancelled")
	writeJSON(w, http.StatusOK, s.maintenanceStatus())
}

func startsBattle(action string) bool {
	return action == economy.ActionBattleStart || action == economy.ActionPvpMatch
}

func (s *Server) heldForMaintenance(action string) error {
	if !startsBattle(action) || !s.maintenance.HoldsBattles() {
		return nil
	}
	return &economy.Refusal{
		Code:    economy.Maintenance,
		Message: "the server is about to restart for an update; battles start again once it is back",
	}
}
