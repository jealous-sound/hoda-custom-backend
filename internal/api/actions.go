package api

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"

	"github.com/jealous-sound/hoda-custom-backend/internal/economy"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
)

const (
	maxActionBodyBytes   = 16384
	purchaseAction       = "purchase"
	refillAction         = "refill"
	guildCreateAction    = "guild_create"
	onslaughtClaimAction = "onslaught_claim"
	namedInBody          = ""
)

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type actionEnvelope struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
}

type actionResponse struct {
	Revision int64           `json:"revision"`
	Profile  json.RawMessage `json:"profile"`
	Granted  json.RawMessage `json:"granted"`
}

type refusalResponse struct {
	Error    string          `json:"error"`
	Code     string          `json:"code"`
	Revision int64           `json:"revision"`
	Profile  json.RawMessage `json:"profile,omitempty"`
}

type action func(record *profile.Record) (granted any, err error)

func (s *Server) runAction(
	w http.ResponseWriter, r *http.Request, player, name string, decode func([]byte) (action, error),
) {
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
	name = cmp.Or(name, envelope.Action)

	var refusal *economy.Refusal
	var badRequest *economy.BadRequest
	var granted json.RawMessage
	stale := false
	record, err := s.profiles.Transact(player, func(record *profile.Record) error {
		if receipt, seen := record.Server.Receipt(envelope.RequestID); seen {
			granted = receipt.Response
			if receipt.Status == http.StatusUnprocessableEntity {
				refusal = &economy.Refusal{}
				var stored refusalResponse
				if err := json.Unmarshal(receipt.Response, &stored); err == nil {
					refusal.Code, refusal.Message = economy.RefusalCode(stored.Code), stored.Error
				}
			}
			return profile.ErrNoChange
		}
		if record.Revision != expected {
			stale = true
			return profile.ErrNoChange
		}
		result, err := apply(record)
		if economy.CommittedRefusal(err) {
			errors.As(err, &refusal)
			response, err := json.Marshal(refusalResponse{Error: refusal.Message, Code: string(refusal.Code)})
			if err != nil {
				return err
			}
			record.Server.Remember(profile.ActionReceipt{
				RequestID: envelope.RequestID, Status: http.StatusUnprocessableEntity, Response: response,
			})
			return nil
		}
		if errors.As(err, &refusal) || errors.As(err, &badRequest) {
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
	case badRequest != nil:
		writeError(w, http.StatusBadRequest, badRequest.Message)
	case stale:
		writeRevision(w, record.Revision)
		writeError(w, http.StatusConflict, "profile was changed since revision "+strconv.FormatInt(expected, 10))
	case refusal != nil:
		if refusal.Code == economy.OutcomeMismatch {
			s.log.Warn("battle outcome mismatch", "player", player, "request", envelope.RequestID)
		}
		s.traffic.Action(player, name, string(refusal.Code))
		writeRevision(w, record.Revision)
		writeJSON(w, http.StatusUnprocessableEntity, refusalResponse{
			Error: refusal.Message, Code: string(refusal.Code), Revision: record.Revision, Profile: record.Profile,
		})
	default:
		s.traffic.Action(player, name, "")
		writeRevision(w, record.Revision)
		writeJSON(w, http.StatusOK, actionResponse{Revision: record.Revision, Profile: record.Profile, Granted: granted})
	}
}

type purchaseBody struct {
	economy.PurchaseRequest
}

func (s *Server) purchase(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, purchaseAction, func(body []byte) (action, error) {
		var request purchaseBody
		if err := json.Unmarshal(body, &request); err != nil || request.Pack == "" {
			return nil, errors.New(`body must be {"request_id", "pack", "currency", "price"}`)
		}
		return func(record *profile.Record) (any, error) {
			next, granted, err := economy.Purchase(s.catalog, record.Profile, &record.Server, request.PurchaseRequest,
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
	Kind economy.RefillKind `json:"kind"`
}

func (s *Server) refill(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, refillAction, func(body []byte) (action, error) {
		var request refillBody
		if err := json.Unmarshal(body, &request); err != nil || request.Kind == "" {
			return nil, errors.New(`body must be {"request_id", "kind"}`)
		}
		return func(record *profile.Record) (any, error) {
			next, err := economy.Refill(s.catalog, record.Profile, &record.Server, request.Kind, s.now())
			if err != nil {
				return nil, err
			}
			record.Profile = next
			return struct {
				Kind economy.RefillKind `json:"kind"`
			}{request.Kind}, nil
		}, nil
	})
}

type genericActionBody struct {
	Action string `json:"action"`
	Duel   string `json:"duel"`
}

func (s *Server) playerAction(w http.ResponseWriter, r *http.Request, player string) {
	s.runAction(w, r, player, namedInBody, func(body []byte) (action, error) {
		var request genericActionBody
		if err := json.Unmarshal(body, &request); err != nil || request.Action == "" {
			return nil, errors.New(`body must be {"request_id", "action", ...}`)
		}
		duels := playerDuels{server: s, player: player, rivals: s.duelRivals(body)}
		return func(record *profile.Record) (any, error) {
			if err := s.heldForMaintenance(request.Action); err != nil {
				return nil, err
			}
			next, result, err := economy.ActWith(s.catalog, record.Profile, &record.Server, request.Action, body,
				s.now(), economy.Env{Duels: duels, Seeds: s.battleSeeds})
			if err != nil && !economy.CommittedRefusal(err) {
				return nil, err
			}
			record.Profile = next
			if request.Action == economy.ActionBattle || request.Action == economy.ActionBattleStart {
				if creditErr := s.creditBattle(record, player, result); creditErr != nil {
					return nil, creditErr
				}
			}
			return result, err
		}, nil
	})
}

func (s *Server) creditBattle(record *profile.Record, player string, result economy.BattleResult) error {
	if err := s.creditOnslaught(record, player, result); err != nil {
		return err
	}
	if result.Settled != nil {
		return s.creditOnslaught(record, player, *result.Settled)
	}
	return nil
}
