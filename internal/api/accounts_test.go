package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jealous-sound/hoda-custom-backend/internal/guild"
	"github.com/jealous-sound/hoda-custom-backend/internal/profile"
	"github.com/jealous-sound/hoda-custom-backend/internal/ratelimit"
)

const (
	hawkePassword   = "andraste's grace"
	anotherPassword = "maker's breath!"
)

type device struct {
	player string
	secret string
	token  string
}

func (h *harness) sessionOf(player, secret string) (*http.Response, string) {
	h.t.Helper()
	request, _ := json.Marshal(sessionRequest{PlayerID: player, Secret: secret})
	response, body := h.do("POST", "/v1/sessions", "", "", string(request))
	var opened sessionResponse
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &opened); err != nil {
			h.t.Fatal(err)
		}
	}
	return response, opened.Token
}

func (h *harness) play(player, secret string) device {
	h.t.Helper()
	response, token := h.sessionOf(player, secret)
	expectStatus(h.t, response, "open session", http.StatusOK)
	return device{player: player, secret: secret, token: token}
}

func (h *harness) guestDevice() device {
	h.t.Helper()
	guest := h.createGuest()
	return h.play(guest.PlayerID, guest.Secret)
}

func (h *harness) account(d device, route, body string) (*http.Response, string) {
	h.t.Helper()
	method := "POST"
	if body == "" {
		method = "GET"
	}
	return h.do(method, "/v1/players/"+d.player+"/account"+route, d.token, "", body)
}

func credentialsBody(login, password string) string {
	body, _ := json.Marshal(signInRequest{Login: login, Password: password})
	return string(body)
}

func (h *harness) registeredDevice(login string) device {
	h.t.Helper()
	d := h.guestDevice()
	response, body := h.account(d, "/login", credentialsBody(login, hawkePassword))
	expectStatus(h.t, response, body, http.StatusOK)
	return d
}

func (h *harness) signIn(login, password string) (*http.Response, string) {
	h.t.Helper()
	return h.do("POST", "/v1/logins", "", "", credentialsBody(login, password))
}

func (h *harness) signedInDevice(login, password string) device {
	h.t.Helper()
	response, body := h.signIn(login, password)
	expectStatus(h.t, response, body, http.StatusOK)
	var reply signInReply
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		h.t.Fatal(err)
	}
	return h.play(reply.PlayerID, reply.Secret)
}

func expectRefusal(t *testing.T, response *http.Response, body string, status int, code string) {
	t.Helper()
	expectStatus(t, response, body, status)
	var refusal accountRefusalReply
	if err := json.Unmarshal([]byte(body), &refusal); err != nil || refusal.Code != code {
		t.Fatalf("refusal = %s, want code %s", body, code)
	}
}

func (h *harness) expectSessionOpen(d device, open bool) {
	h.t.Helper()
	want := http.StatusOK
	if !open {
		want = http.StatusUnauthorized
	}
	response, body := h.account(d, "", "")
	expectStatus(h.t, response, body, want)
}

func TestGuestChoosesALoginAndSignsInElsewhere(t *testing.T) {
	h := newHarness(t)
	home := h.guestDevice()
	response, body := h.account(home, "", "")
	expectStatus(t, response, body, http.StatusOK)
	var before accountReply
	if err := json.Unmarshal([]byte(body), &before); err != nil {
		t.Fatal(err)
	}
	if before.PlayerID != home.player || before.Login != "" || before.Devices != 1 || before.CreatedAt != fixedNow.Unix() {
		t.Fatalf("guest account = %+v", before)
	}

	response, body = h.account(home, "/login", credentialsBody("Hawke", hawkePassword))
	expectStatus(t, response, body, http.StatusOK)
	laptop := h.signedInDevice("hawke", hawkePassword)
	if laptop.player != home.player || laptop.secret == home.secret {
		t.Fatalf("second device = %+v, first = %+v", laptop, home)
	}
	response, body = h.account(laptop, "", "")
	expectStatus(t, response, body, http.StatusOK)
	var after accountReply
	if err := json.Unmarshal([]byte(body), &after); err != nil {
		t.Fatal(err)
	}
	if after.Login != "Hawke" || after.Devices != 2 {
		t.Fatalf("account after sign-in = %+v", after)
	}
	h.expectSessionOpen(home, true)
}

func TestLoginChoiceIsRefusedWithACode(t *testing.T) {
	h := newHarness(t)
	owner := h.registeredDevice("Hawke")
	rival := h.guestDevice()
	for name, attempt := range map[string]struct {
		device device
		body   string
		status int
		code   string
	}{
		"taken":          {rival, credentialsBody("HAWKE", hawkePassword), http.StatusConflict, accountCodeLoginTaken},
		"second login":   {owner, credentialsBody("Champion", hawkePassword), http.StatusConflict, accountCodeHasLogin},
		"bad login":      {rival, credentialsBody("grey warden", hawkePassword), http.StatusUnprocessableEntity, accountCodeInvalidLogin},
		"short password": {rival, credentialsBody("Varric", "bianca"), http.StatusUnprocessableEntity, accountCodeInvalidPassword},
	} {
		t.Run(name, func(t *testing.T) {
			response, body := h.account(attempt.device, "/login", attempt.body)
			expectRefusal(t, response, body, attempt.status, attempt.code)
		})
	}
	response, body := h.account(rival, "/login", `not json`)
	expectStatus(t, response, body, http.StatusBadRequest)
}

func TestSignInRefusesWrongLoginOrPasswordAlike(t *testing.T) {
	h := newHarness(t)
	h.registeredDevice("Hawke")
	_, wrongPassword := h.signIn("Hawke", anotherPassword)
	_, unknownLogin := h.signIn("Varric", hawkePassword)
	_, malformedLogin := h.signIn("not a login", hawkePassword)
	for _, attempt := range [][2]string{{"Hawke", anotherPassword}, {"Varric", hawkePassword}, {"not a login", hawkePassword}} {
		response, body := h.signIn(attempt[0], attempt[1])
		expectRefusal(t, response, body, http.StatusUnauthorized, accountCodeWrongCredentials)
	}
	if wrongPassword != unknownLogin || wrongPassword != malformedLogin {
		t.Fatalf("refusals tell a known login apart: %q %q %q", wrongPassword, unknownLogin, malformedLogin)
	}
}

func TestSignInGuessesAreLimitedPerLoginAndPerAddress(t *testing.T) {
	for name, limits := range map[string]Limits{
		"per login":   {SignInsPerLogin: ratelimit.Rule{Burst: 2, Every: time.Hour}},
		"per address": {SignInsPerAddress: ratelimit.Rule{Burst: 2, Every: time.Hour}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarnessWith(t, Config{Limits: limits})
			h.registeredDevice("Hawke")
			for range 2 {
				response, body := h.signIn("hawke", anotherPassword)
				expectStatus(t, response, body, http.StatusUnauthorized)
			}
			response, body := h.signIn("Hawke", hawkePassword)
			expectStatus(t, response, body, http.StatusTooManyRequests)
		})
	}
}

func TestPasswordGuessesOnAnOpenSessionAreLimited(t *testing.T) {
	h := newHarnessWith(t, Config{Limits: Limits{PasswordsPerPlayer: ratelimit.Rule{Burst: 2, Every: time.Hour}}})
	d := h.guestDevice()
	response, body := h.account(d, "/login", credentialsBody("Hawke", hawkePassword))
	expectStatus(t, response, body, http.StatusOK)
	response, body = h.account(d, "/delete", `{"password":"wrong guess"}`)
	expectRefusal(t, response, body, http.StatusForbidden, accountCodeWrongPassword)
	response, body = h.account(d, "/delete", `{"password":"`+hawkePassword+`"}`)
	expectStatus(t, response, body, http.StatusTooManyRequests)
}

func TestChangingThePasswordKeepsThisDeviceAndSignsOutTheRest(t *testing.T) {
	h := newHarness(t)
	home := h.registeredDevice("Hawke")
	laptop := h.signedInDevice("Hawke", hawkePassword)
	change := func(current, next string) (*http.Response, string) {
		request, _ := json.Marshal(changePasswordRequest{CurrentPassword: current, NewPassword: next, Secret: laptop.secret})
		return h.account(laptop, "/password", string(request))
	}
	response, body := change(anotherPassword, anotherPassword)
	expectRefusal(t, response, body, http.StatusForbidden, accountCodeWrongPassword)
	response, body = change(hawkePassword, "short")
	expectRefusal(t, response, body, http.StatusUnprocessableEntity, accountCodeInvalidPassword)
	h.expectSessionOpen(home, true)

	response, body = change(hawkePassword, anotherPassword)
	expectStatus(t, response, body, http.StatusOK)
	h.expectSessionOpen(laptop, true)
	h.expectSessionOpen(home, false)
	if response, _ := h.sessionOf(home.player, home.secret); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed-out device reopened a session: %d", response.StatusCode)
	}
	if response, _ := h.sessionOf(laptop.player, laptop.secret); response.StatusCode != http.StatusOK {
		t.Fatalf("the device that changed the password was signed out: %d", response.StatusCode)
	}
	response, body = h.signIn("Hawke", hawkePassword)
	expectRefusal(t, response, body, http.StatusUnauthorized, accountCodeWrongCredentials)
	h.signedInDevice("Hawke", anotherPassword)
}

func TestSignOutEndsThisDeviceOnly(t *testing.T) {
	h := newHarness(t)
	home := h.registeredDevice("Hawke")
	laptop := h.signedInDevice("Hawke", hawkePassword)
	response, body := h.account(laptop, "/logout", `{"secret":"`+laptop.secret+`"}`)
	expectStatus(t, response, body, http.StatusOK)
	h.expectSessionOpen(laptop, false)
	if response, _ := h.sessionOf(laptop.player, laptop.secret); response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signed-out device reopened a session: %d", response.StatusCode)
	}
	h.expectSessionOpen(home, true)
	h.signedInDevice("Hawke", hawkePassword)
}

func TestGuestCannotSignOutOfItsOnlyWayIn(t *testing.T) {
	h := newHarness(t)
	guest := h.guestDevice()
	response, body := h.account(guest, "/logout", `{"secret":"`+guest.secret+`"}`)
	expectRefusal(t, response, body, http.StatusConflict, accountCodeNoLogin)
	h.expectSessionOpen(guest, true)
	request, _ := json.Marshal(changePasswordRequest{NewPassword: anotherPassword, Secret: guest.secret})
	response, body = h.account(guest, "/password", string(request))
	expectRefusal(t, response, body, http.StatusConflict, accountCodeNoLogin)
}

func TestPlayerWithoutAnAccountHasNoAccountRoutes(t *testing.T) {
	h := newHarness(t)
	local := device{player: "local", token: h.open("local")}
	response, body := h.account(local, "", "")
	expectRefusal(t, response, body, http.StatusNotFound, accountCodeNoAccount)
	response, body = h.account(local, "/login", credentialsBody("Hawke", hawkePassword))
	expectRefusal(t, response, body, http.StatusNotFound, accountCodeNoAccount)
}

func TestAccountRoutesBelongToTheSessionPlayer(t *testing.T) {
	h := newHarness(t)
	owner := h.registeredDevice("Hawke")
	intruder := h.guestDevice()
	response, body := h.do("POST", "/v1/players/"+owner.player+"/account/delete", intruder.token, "", `{"password":""}`)
	expectStatus(t, response, body, http.StatusForbidden)
	response, body = h.do("GET", "/v1/players/"+owner.player+"/account", "", "", "")
	expectStatus(t, response, body, http.StatusUnauthorized)
	h.expectSessionOpen(owner, true)
}

func (h *harness) guildMembers() map[string]guild.Rank {
	members := map[string]guild.Rank{}
	h.guildsOf(func(g *guild.Guild) {
		for _, member := range g.Members {
			members[member.PlayerID] = member.Rank
		}
	})
	return members
}

func (h *harness) guildsOf(visit func(g *guild.Guild)) {
	h.t.Helper()
	store, err := guild.NewStore(h.dir)
	if err != nil {
		h.t.Fatal(err)
	}
	store.View(func(state *guild.State) {
		for _, g := range state.Guilds {
			visit(g)
		}
	})
}

func (h *harness) storeProfile(d device, name string) {
	h.t.Helper()
	if _, err := h.profiles.Put(d.player, 0, json.RawMessage(guildProfile(name, 9, 12000))); err != nil {
		h.t.Fatal(err)
	}
}

func TestDeletingAnAccountNeedsItsPasswordAndRemovesThePlayer(t *testing.T) {
	h := newHarnessWith(t, Config{})
	hawke := h.registeredDevice("Hawke")
	h.storeProfile(hawke, "Hawke")
	laptop := h.signedInDevice("Hawke", hawkePassword)
	varric := h.guestDevice()
	h.storeProfile(varric, "Varric")

	response, body := h.do("POST", "/v1/players/"+hawke.player+"/guilds", hawke.token, "1", createOpenGuild)
	expectStatus(t, response, body, http.StatusOK)
	var guildID string
	h.guildsOf(func(g *guild.Guild) { guildID = g.ID })
	response, body = h.do("POST", "/v1/players/"+varric.player+"/guilds/"+guildID+"/join", varric.token, "", `{}`)
	expectStatus(t, response, body, http.StatusOK)

	response, body = h.account(hawke, "/delete", `{"password":"`+anotherPassword+`"}`)
	expectRefusal(t, response, body, http.StatusForbidden, accountCodeWrongPassword)
	if _, err := h.profiles.Get(hawke.player); err != nil {
		t.Fatalf("a refused deletion removed the profile: %v", err)
	}

	response, body = h.account(hawke, "/delete", `{"password":"`+hawkePassword+`"}`)
	expectStatus(t, response, body, http.StatusOK)
	if _, err := h.profiles.Get(hawke.player); !errors.Is(err, profile.ErrNotFound) {
		t.Fatalf("profile of a deleted account: %v", err)
	}
	h.expectSessionOpen(hawke, false)
	h.expectSessionOpen(laptop, false)
	if response, _ := h.sessionOf(laptop.player, laptop.secret); response.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted account opened a session: %d", response.StatusCode)
	}
	response, body = h.signIn("Hawke", hawkePassword)
	expectRefusal(t, response, body, http.StatusUnauthorized, accountCodeWrongCredentials)
	if members := h.guildMembers(); len(members) != 1 || members[varric.player] != guild.RankLeader {
		t.Fatalf("guild after its leader was deleted: %+v", members)
	}

	response, body = h.account(varric, "/delete", `{"password":""}`)
	expectStatus(t, response, body, http.StatusOK)
	if members := h.guildMembers(); len(members) != 0 {
		t.Fatalf("guild outlived its last member: %+v", members)
	}
}

func (h *harness) resetPassword(login string) (*http.Response, passwordResetReply) {
	h.t.Helper()
	response, body := h.do("PUT", adminLoginsPath+"/"+login+"/password", testAdminToken, "", "")
	var reply passwordResetReply
	if response.StatusCode == http.StatusOK {
		if err := json.Unmarshal([]byte(body), &reply); err != nil {
			h.t.Fatal(err)
		}
	}
	return response, reply
}

func TestOperatorResetsAForgottenPassword(t *testing.T) {
	h := newMaintenanceHarness(t)
	home := h.registeredDevice("Hawke")
	if listed := h.listedPlayers()[home.player]; listed.Login != "Hawke" {
		t.Fatalf("listed player = %+v", listed)
	}
	response, reset := h.resetPassword("hawke")
	if response.StatusCode != http.StatusOK || reset.PlayerID != home.player || reset.Login != "Hawke" {
		t.Fatalf("reset = %d %+v", response.StatusCode, reset)
	}
	h.expectSessionOpen(home, false)
	response, body := h.signIn("Hawke", hawkePassword)
	expectRefusal(t, response, body, http.StatusUnauthorized, accountCodeWrongCredentials)
	h.signedInDevice("Hawke", reset.Password)

	if response, _ := h.resetPassword("Varric"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("reset of an unknown login: %d", response.StatusCode)
	}
	response, body = h.do("PUT", adminLoginsPath+"/hawke/password", "", "", "")
	expectStatus(t, response, body, http.StatusUnauthorized)
}
