package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultWebOrigins      = "https://hodareborn.com"
	preflightRoute         = "preflight"
	webAllowedMethods      = "GET, POST, PUT"
	webAllowedHeaders      = "Authorization, Content-Type, If-Match"
	webPreflightCacheFor   = 2 * time.Hour
	webOriginListSeparator = ","
)

func ParseWebOrigins(list string) ([]string, error) {
	var origins []string
	for _, field := range strings.Split(list, webOriginListSeparator) {
		origin := strings.TrimSpace(field)
		if origin == "" {
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" ||
			parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return nil, fmt.Errorf("web origin %q must be a scheme and host such as %s", origin, DefaultWebOrigins)
		}
		origins = append(origins, origin)
	}
	return origins, nil
}

func originSet(origins []string) map[string]bool {
	set := make(map[string]bool, len(origins))
	for _, origin := range origins {
		set[origin] = true
	}
	return set
}

func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
}

func (s *Server) crossOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, versionedPathPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		header := w.Header()
		header.Add("Vary", "Origin")
		origin := r.Header.Get("Origin")
		allowed := s.webOrigins[origin]
		if isPreflight(r) {
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			header.Set("Access-Control-Allow-Origin", origin)
			header.Set("Access-Control-Allow-Methods", webAllowedMethods)
			header.Set("Access-Control-Allow-Headers", webAllowedHeaders)
			header.Set("Access-Control-Max-Age", strconv.Itoa(int(webPreflightCacheFor/time.Second)))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if allowed {
			header.Set("Access-Control-Allow-Origin", origin)
			header.Set("Access-Control-Expose-Headers", RevisionHeader)
		}
		next.ServeHTTP(w, r)
	})
}
