package api

import (
	"net/http"
	"strings"

	"github.com/jealous-sound/hoda-custom-backend/internal/clientversion"
)

const (
	clientUpdatePath    = "/v1/client"
	versionedPathPrefix = "/v1/"
)

type ClientUpdates struct {
	Minimum     clientversion.Version
	Latest      func() clientversion.Version
	DownloadURL string
}

type clientUpdateResponse struct {
	Latest      string `json:"latest"`
	Minimum     string `json:"minimum"`
	DownloadURL string `json:"download_url"`
	maintenanceResponse
}

type clientTooOldResponse struct {
	Error string `json:"error"`
	clientUpdateResponse
}

func (s *Server) clientUpdate() clientUpdateResponse {
	latest := clientversion.Version{}
	if s.clientUpdates.Latest != nil {
		latest = s.clientUpdates.Latest()
	}
	minimum := s.clientUpdates.Minimum
	return clientUpdateResponse{
		Latest:      clientversion.Newest(latest, minimum).String(),
		Minimum:     minimum.String(),
		DownloadURL: s.clientUpdates.DownloadURL, maintenanceResponse: s.maintenanceStatus(),
	}
}

func (s *Server) clientRelease(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.clientUpdate())
}

func (s *Server) currentClientsOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.clientTooOld(r) {
			writeJSON(w, http.StatusUpgradeRequired, clientTooOldResponse{
				Error:                "this client version is no longer supported; download " + s.clientUpdate().Latest,
				clientUpdateResponse: s.clientUpdate(),
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) clientTooOld(r *http.Request) bool {
	minimum := s.clientUpdates.Minimum
	if !minimum.Known() || r.URL.Path == clientUpdatePath || !strings.HasPrefix(r.URL.Path, versionedPathPrefix) {
		return false
	}
	version, ok := clientversion.FromUserAgent(r.Header.Get("User-Agent"))
	return ok && version.Older(minimum)
}
