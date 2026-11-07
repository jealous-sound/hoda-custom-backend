package ratelimit

import (
	"sync"
	"time"
)

const sweepEvery = time.Minute

type Rule struct {
	Burst int
	Every time.Duration
}

func (r Rule) refillTime() time.Duration {
	return r.Every * time.Duration(r.Burst)
}

type bucket struct {
	tokens  float64
	updated time.Time
}

type Limiter struct {
	rule      Rule
	now       func() time.Time
	mutex     sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

func New(rule Rule, now func() time.Time) *Limiter {
	return &Limiter{rule: rule, now: now, buckets: map[string]*bucket{}, lastSweep: now()}
}

func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil || l.rule.Burst <= 0 || l.rule.Every <= 0 {
		return true, 0
	}
	now := l.now()
	l.mutex.Lock()
	defer l.mutex.Unlock()
	l.sweepLocked(now)
	current, ok := l.buckets[key]
	if !ok {
		current = &bucket{tokens: float64(l.rule.Burst), updated: now}
		l.buckets[key] = current
	}
	refilled := float64(now.Sub(current.updated)) / float64(l.rule.Every)
	current.tokens = min(float64(l.rule.Burst), current.tokens+max(refilled, 0))
	current.updated = now
	if current.tokens >= 1 {
		current.tokens--
		return true, 0
	}
	return false, time.Duration((1 - current.tokens) * float64(l.rule.Every))
}

func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.lastSweep = now
	full := l.rule.refillTime()
	for key, entry := range l.buckets {
		if now.Sub(entry.updated) >= full {
			delete(l.buckets, key)
		}
	}
}

func (l *Limiter) tracked() int {
	l.mutex.Lock()
	defer l.mutex.Unlock()
	return len(l.buckets)
}
