package api

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/ratelimit"
)

type Limits struct {
	AccountsPerAddress ratelimit.Rule
	AccountsTotal      ratelimit.Rule
	SessionsPerAddress ratelimit.Rule
	RequestsPerPlayer  ratelimit.Rule
	ChatPerPlayer      ratelimit.Rule
	WorldChatPerPlayer ratelimit.Rule
	ReportsPerPlayer   ratelimit.Rule
	AdminPerAddress    ratelimit.Rule
}

func DefaultLimits() Limits {
	return Limits{
		AccountsPerAddress: ratelimit.Rule{Burst: 10, Every: 6 * time.Minute},
		AccountsTotal:      ratelimit.Rule{Burst: 120, Every: 10 * time.Second},
		SessionsPerAddress: ratelimit.Rule{Burst: 30, Every: 10 * time.Second},
		RequestsPerPlayer:  ratelimit.Rule{Burst: 60, Every: 100 * time.Millisecond},
		ChatPerPlayer:      ratelimit.Rule{Burst: 5, Every: 3 * time.Second},
		WorldChatPerPlayer: ratelimit.Rule{Burst: 3, Every: 5 * time.Second},
		ReportsPerPlayer:   ratelimit.Rule{Burst: 5, Every: 12 * time.Minute},
		AdminPerAddress:    ratelimit.Rule{Burst: 10, Every: 30 * time.Second},
	}
}

type limiters struct {
	accountsByAddress *ratelimit.Limiter
	accountsTotal     *ratelimit.Limiter
	sessionsByAddress *ratelimit.Limiter
	requestsByPlayer  *ratelimit.Limiter
	chatByPlayer      *ratelimit.Limiter
	worldChatByPlayer *ratelimit.Limiter
	reportsByPlayer   *ratelimit.Limiter
	adminByAddress    *ratelimit.Limiter
}

func newLimiters(limits Limits, now func() time.Time) limiters {
	return limiters{
		accountsByAddress: ratelimit.New(limits.AccountsPerAddress, now),
		accountsTotal:     ratelimit.New(limits.AccountsTotal, now),
		sessionsByAddress: ratelimit.New(limits.SessionsPerAddress, now),
		requestsByPlayer:  ratelimit.New(limits.RequestsPerPlayer, now),
		chatByPlayer:      ratelimit.New(limits.ChatPerPlayer, now),
		worldChatByPlayer: ratelimit.New(limits.WorldChatPerPlayer, now),
		reportsByPlayer:   ratelimit.New(limits.ReportsPerPlayer, now),
		adminByAddress:    ratelimit.New(limits.AdminPerAddress, now),
	}
}

func (s *Server) allow(w http.ResponseWriter, limiter *ratelimit.Limiter, key string) bool {
	allowed, retry := limiter.Allow(key)
	if allowed {
		return true
	}
	seconds := max(int(math.Ceil(retry.Seconds())), 1)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeError(w, http.StatusTooManyRequests, "too many requests, try again in "+strconv.Itoa(seconds)+"s")
	return false
}
