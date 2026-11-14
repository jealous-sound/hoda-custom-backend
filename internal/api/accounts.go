package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jealous-sound/hoda-custom-backend/internal/account"
	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
)

const (
	maxAccountBodyBytes = 4096

	accountCodeInvalidLogin     = "invalid_login"
	accountCodeInvalidPassword  = "invalid_password"
	accountCodeLoginTaken       = "login_taken"
	accountCodeHasLogin         = "has_login"
	accountCodeNoLogin          = "no_login"
	accountCodeWrongPassword    = "wrong_password"
	accountCodeWrongCredentials = "wrong_credentials"
	accountCodeNoAccount        = "no_account"
)

type accountReply struct {
	PlayerID  string `json:"player_id"`
	Login     string `json:"login"`
	CreatedAt int64  `json:"created_at"`
	Devices   int    `json:"devices"`
}

type accountRefusalReply struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type signInRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type signInReply struct {
	PlayerID   string `json:"player_id"`
	Secret     string `json:"secret"`
	ServerTime int64  `json:"server_time"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	Secret          string `json:"secret"`
}

type signOutRequest struct {
	Secret string `json:"secret"`
}

type deleteAccountRequest struct {
	Password string `json:"password"`
}

func (s *Server) accountRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/logins", s.signIn)
	mux.HandleFunc("GET /v1/players/{player}/account", s.authorized(s.showAccount))
	mux.HandleFunc("POST /v1/players/{player}/account/login", s.authorized(s.chooseLogin))
	mux.HandleFunc("POST /v1/players/{player}/account/password", s.authorized(s.changePassword))
	mux.HandleFunc("POST /v1/players/{player}/account/logout", s.authorized(s.signOut))
	mux.HandleFunc("POST /v1/players/{player}/account/delete", s.authorized(s.deleteAccount))
}

func describeAccount(record account.Account) accountReply {
	return accountReply{
		PlayerID: record.PlayerID, Login: record.Login, CreatedAt: unixOrZero(record.CreatedAt),
		Devices: record.DeviceCount(),
	}
}

func bearerToken(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return token
}

func readAccountBody(w http.ResponseWriter, r *http.Request, request any, shape string) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, maxAccountBodyBytes)).Decode(request); err != nil {
		writeError(w, http.StatusBadRequest, "body must be "+shape)
		return false
	}
	return true
}

func writeAccountRefusal(w http.ResponseWriter, status int, code string, err error) {
	writeJSON(w, status, accountRefusalReply{Error: err.Error(), Code: code})
}

func (s *Server) refuseAccountChange(w http.ResponseWriter, player string, err error) {
	switch {
	case errors.Is(err, account.ErrInvalidLogin):
		writeAccountRefusal(w, http.StatusUnprocessableEntity, accountCodeInvalidLogin, err)
	case errors.Is(err, account.ErrInvalidPassword):
		writeAccountRefusal(w, http.StatusUnprocessableEntity, accountCodeInvalidPassword, err)
	case errors.Is(err, account.ErrLoginTaken):
		writeAccountRefusal(w, http.StatusConflict, accountCodeLoginTaken, err)
	case errors.Is(err, account.ErrHasLogin):
		writeAccountRefusal(w, http.StatusConflict, accountCodeHasLogin, err)
	case errors.Is(err, account.ErrNoLogin):
		writeAccountRefusal(w, http.StatusConflict, accountCodeNoLogin, err)
	case errors.Is(err, account.ErrWrongPassword):
		writeAccountRefusal(w, http.StatusForbidden, accountCodeWrongPassword, err)
	case errors.Is(err, account.ErrNotRegistered):
		writeAccountRefusal(w, http.StatusNotFound, accountCodeNoAccount, err)
	default:
		s.log.Error("change account", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not change the account")
	}
}

func (s *Server) signIn(w http.ResponseWriter, r *http.Request) {
	if !s.allow(w, s.limiters.signInsByAddress, s.visitor(r)) || !s.allow(w, s.limiters.signInsTotal, "") {
		return
	}
	var request signInRequest
	if !readAccountBody(w, r, &request, `{"login": "...", "password": "..."}`) {
		return
	}
	if !account.ValidLogin(request.Login) {
		writeAccountRefusal(w, http.StatusUnauthorized, accountCodeWrongCredentials, account.ErrWrongCredentials)
		return
	}
	if !s.allow(w, s.limiters.signInsByLogin, account.LoginKey(request.Login)) {
		return
	}
	credentials, err := s.accounts.SignIn(request.Login, request.Password)
	if errors.Is(err, account.ErrWrongCredentials) {
		writeAccountRefusal(w, http.StatusUnauthorized, accountCodeWrongCredentials, err)
		return
	}
	if err != nil {
		s.log.Error("sign in", "error", err)
		writeError(w, http.StatusInternalServerError, "could not sign in")
		return
	}
	s.log.Info("signed in on a device", "player", credentials.PlayerID)
	writeJSON(w, http.StatusOK, signInReply{
		PlayerID: credentials.PlayerID, Secret: credentials.Secret, ServerTime: s.now().Unix(),
	})
}

func (s *Server) showAccount(w http.ResponseWriter, _ *http.Request, player string) {
	record, err := s.accounts.Find(player)
	if err != nil {
		s.refuseAccountChange(w, player, err)
		return
	}
	writeJSON(w, http.StatusOK, describeAccount(record))
}

func (s *Server) chooseLogin(w http.ResponseWriter, r *http.Request, player string) {
	var request signInRequest
	if !readAccountBody(w, r, &request, `{"login": "...", "password": "..."}`) ||
		!s.allow(w, s.limiters.passwordsByPlayer, player) {
		return
	}
	record, err := s.accounts.SetLogin(player, request.Login, request.Password)
	if err != nil {
		s.refuseAccountChange(w, player, err)
		return
	}
	s.log.Info("player chose a login", "player", player)
	writeJSON(w, http.StatusOK, describeAccount(record))
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, player string) {
	var request changePasswordRequest
	shape := `{"current_password": "...", "new_password": "...", "secret": "..."}`
	if !readAccountBody(w, r, &request, shape) || !s.allow(w, s.limiters.passwordsByPlayer, player) {
		return
	}
	record, err := s.accounts.ChangePassword(player, request.CurrentPassword, request.NewPassword, request.Secret)
	if err != nil {
		s.refuseAccountChange(w, player, err)
		return
	}
	s.sessions.CloseOthers(player, bearerToken(r))
	s.log.Info("player changed the password", "player", player)
	writeJSON(w, http.StatusOK, describeAccount(record))
}

func (s *Server) signOut(w http.ResponseWriter, r *http.Request, player string) {
	var request signOutRequest
	if !readAccountBody(w, r, &request, `{"secret": "..."}`) {
		return
	}
	if err := s.accounts.SignOut(player, request.Secret); err != nil {
		s.refuseAccountChange(w, player, err)
		return
	}
	s.sessions.Close(bearerToken(r))
	s.log.Info("signed out on a device", "player", player)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, player string) {
	var request deleteAccountRequest
	if !readAccountBody(w, r, &request, `{"password": "..."}`) || !s.allow(w, s.limiters.passwordsByPlayer, player) {
		return
	}
	if err := s.accounts.ConfirmOwner(player, request.Password); err != nil {
		s.refuseAccountChange(w, player, err)
		return
	}
	if err := s.erasePlayer(player); err != nil {
		s.log.Error("delete account", "player", player, "error", err)
		writeError(w, http.StatusInternalServerError, "could not delete the account")
		return
	}
	s.log.Info("player deleted the account", "player", player)
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) erasePlayer(player string) error {
	now := s.now().Unix()
	err := s.guilds.Update(func(state *guild.State) error {
		state.RemovePlayer(player, now)
		return nil
	})
	if err != nil {
		return err
	}
	if err := s.profiles.Delete(player); err != nil {
		return err
	}
	if err := s.accounts.Delete(player); err != nil {
		return err
	}
	s.sessions.CloseAll(player)
	return nil
}
