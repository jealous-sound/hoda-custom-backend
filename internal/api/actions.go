package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/shop"
)

const maxActionBodyBytes = 4096

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type actionEnvelope struct {
	RequestID string `json:"request_id"`
}

type actionResponse struct {
	Revision int64           `json:"revision"`
	Profile  json.RawMessage `json:"profile"`
	Granted  json.RawMessage `json:"granted"`
}

type refusalResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type action func(record *profile.Record) (granted any, err error)

func (s *Server) runAction(w http.ResponseWriter, r *http.Request, player string, decode func([]byte) (action, error)) {
	expected, ok := parseIfMatch(r.Header.Get("If-Match"))
	if !ok {
		writeError(w, http.StatusPreconditionRequired, "If-Match with the last seen revision is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxActionBodyBytes+1))
	if err != nil || len(body) > maxActionBodyBytes {
		writeError(w, http.StatusBadRequest, "could not read body")
		return
	}
	var envelope actionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || !requestIDPattern.MatchString(envelope.RequestID) {
		writeError(w, http.StatusBadRequest, "request_id must be 8-64 letters, digits, '-' or '_'")
		return
	}
	apply, err := decode(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var refusal *shop.Refusal
	var granted json.RawMessage
	stale := false
	record, err := s.profiles.Transact(player, func(record *profile.Record) error {
		if receipt, seen := record.Server.Receipt(envelope.RequestID); seen {
			granted = receipt.Response
			return profile.ErrNoChange
		}
		if record.Revision != expected {
			stale = true
			return profile.ErrNoChange
		}
		result, err := apply(record)
		if errors.As(err, &refusal) {
			return profile.ErrNoChange
		}
		if err != nil {
			return err
		}
		granted, err = json.Marshal(result)
		if err != nil {
			return err
		}
		record.Server.Remember(profile.ActionReceipt{
			RequestID: envelope.RequestID, Status: http.StatusOK, Response: granted,
		})
		return nil
	})
	switch {
	case errors.Is(err, profile.ErrNotFound):
		writeError(w, http.StatusNotFound, "no profile yet")
	case err != nil && !errors.Is(err, profile.ErrNoChange):
		s.log.Error("player action", "player", player, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "could not apply action")
	case stale:
		writeRevision(w, record.Revision)
		writeError(w, http.StatusConflict, "profile was changed since revision "+strconv.FormatInt(expected, 10))
	case refusal != nil:
		writeRevision(w, record.Revision)
		writeJSON(w, http.StatusUnprocessableEntity, refusalResponse{Error: refusal.Message, Code: string(refusal.Code)})
	default:
		writeRevision(w, record.Revision)
		writeJSON(w, http.StatusOK, actionResponse{Revision: record.Revision, Profile: record.Profile, Granted: granted})
	}
}

type purchaseBody struct {
	shop.PurchaseRequest
}

func (s *Server) purchase(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, func(body []byte) (action, error) {
		var request purchaseBody
		if err := json.Unmarshal(body, &request); err != nil || request.Pack == "" {
			return nil, errors.New(`body must be {"request_id", "pack", "currency", "price"}`)
		}
		return func(record *profile.Record) (any, error) {
			next, granted, err := shop.Purchase(s.catalog, record.Profile, &record.Server, request.PurchaseRequest,
				s.now())
			if err != nil {
				return nil, err
			}
			record.Profile = next
			return granted, nil
		}, nil
	})
}

type refillBody struct {
	Kind shop.RefillKind `json:"kind"`
}

func (s *Server) refill(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, func(body []byte) (action, error) {
		var request refillBody
		if err := json.Unmarshal(body, &request); err != nil || request.Kind == "" {
			return nil, errors.New(`body must be {"request_id", "kind"}`)
		}
		return func(record *profile.Record) (any, error) {
			next, err := shop.Refill(record.Profile, request.Kind)
			if err != nil {
				return nil, err
			}
			record.Profile = next
			return struct {
				Kind shop.RefillKind `json:"kind"`
			}{request.Kind}, nil
		}, nil
	})
}
