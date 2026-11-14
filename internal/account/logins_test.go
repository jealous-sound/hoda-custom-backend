package account

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	quickPasswordIterations = 4
	password                = "andraste's grace"
	otherPassword           = "maker's breath!"
)

type world struct {
	t     *testing.T
	dir   string
	clock time.Time
	store *Store
}

func newWorld(t *testing.T) *world {
	w := &world{t: t, dir: t.TempDir(), clock: time.Unix(1_800_000_000, 0).UTC()}
	w.store = w.reopen()
	return w
}

func (w *world) reopen() *Store {
	w.t.Helper()
	store, err := NewStore(w.dir, func() time.Time { return w.clock }, WithPasswordIterations(quickPasswordIterations))
	if err != nil {
		w.t.Fatal(err)
	}
	return store
}

func (w *world) guest() Credentials {
	w.t.Helper()
	credentials, err := w.store.CreateGuest()
	if err != nil {
		w.t.Fatal(err)
	}
	return credentials
}

func (w *world) registered(login string) Credentials {
	w.t.Helper()
	credentials := w.guest()
	if _, err := w.store.SetLogin(credentials.PlayerID, login, password); err != nil {
		w.t.Fatal(err)
	}
	return credentials
}

func (w *world) signIn(login, secret string) Credentials {
	w.t.Helper()
	credentials, err := w.store.SignIn(login, secret)
	if err != nil {
		w.t.Fatal(err)
	}
	return credentials
}

func (w *world) expectHeld(credentials Credentials, held bool) {
	w.t.Helper()
	verified, err := w.store.Verify(credentials.PlayerID, credentials.Secret)
	if err != nil {
		w.t.Fatal(err)
	}
	if verified != held {
		w.t.Fatalf("device key accepted = %v, want %v", verified, held)
	}
}

func TestLoginRules(t *testing.T) {
	for _, login := range []string{"Hawke", "grey.warden-9", "abc", strings.Repeat("a", MaxLoginLength)} {
		if !ValidLogin(login) {
			t.Errorf("%q refused", login)
		}
	}
	for _, login := range []string{"", "ab", "-hawke", "grey warden", "хоук", "a@b.c", strings.Repeat("a", MaxLoginLength+1)} {
		if ValidLogin(login) {
			t.Errorf("%q accepted", login)
		}
	}
	if ValidPassword("seven77") || !ValidPassword("eight888") || ValidPassword(strings.Repeat("p", MaxPasswordLength+1)) {
		t.Error("password length limits are off")
	}
	if !ValidPassword("пароль-длиннее") {
		t.Error("a password in another script was refused")
	}
}

func TestSignInOpensAnotherDeviceOnTheSameAccount(t *testing.T) {
	w := newWorld(t)
	first := w.registered("Hawke")
	second := w.signIn("hawke", password)
	if second.PlayerID != first.PlayerID {
		t.Fatalf("signed in as %s, want %s", second.PlayerID, first.PlayerID)
	}
	if second.Secret == first.Secret {
		t.Fatal("the second device shares the first device's key")
	}
	w.expectHeld(first, true)
	w.expectHeld(second, true)
}

func TestSignInRefusesWrongPasswordAndUnknownLogin(t *testing.T) {
	w := newWorld(t)
	w.registered("Hawke")
	for _, attempt := range [][2]string{{"Hawke", otherPassword}, {"Varric", password}, {"Varric", decoyPassword}} {
		if _, err := w.store.SignIn(attempt[0], attempt[1]); !errors.Is(err, ErrWrongCredentials) {
			t.Errorf("sign in as %q: %v", attempt[0], err)
		}
	}
}

func TestLoginBelongsToOnePlayer(t *testing.T) {
	w := newWorld(t)
	owner := w.registered("Hawke")
	rival := w.guest()
	if _, err := w.store.SetLogin(rival.PlayerID, "HAWKE", password); !errors.Is(err, ErrLoginTaken) {
		t.Fatalf("taking a used login: %v", err)
	}
	if _, err := w.store.SetLogin(owner.PlayerID, "Champion", password); !errors.Is(err, ErrHasLogin) {
		t.Fatalf("choosing a second login: %v", err)
	}
	if _, err := w.store.SetLogin(rival.PlayerID, "ab", password); !errors.Is(err, ErrInvalidLogin) {
		t.Fatalf("short login: %v", err)
	}
	if _, err := w.store.SetLogin(rival.PlayerID, "Varric", "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("short password: %v", err)
	}
	if _, err := w.store.SetLogin("guest-missing", "Varric", password); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("login for an unknown player: %v", err)
	}
}

func TestLoginsSurviveARestart(t *testing.T) {
	w := newWorld(t)
	owner := w.registered("Hawke")
	w.store = w.reopen()
	if again := w.signIn("Hawke", password); again.PlayerID != owner.PlayerID {
		t.Fatalf("signed in as %s after a restart", again.PlayerID)
	}
	if _, err := w.store.SetLogin(w.guest().PlayerID, "hawke", password); !errors.Is(err, ErrLoginTaken) {
		t.Fatalf("login was free after a restart: %v", err)
	}
}

func TestStoredPasswordIsSaltedAndNotThePassword(t *testing.T) {
	w := newWorld(t)
	first, err := w.store.Find(w.registered("Hawke").PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.store.Find(w.registered("Varric").PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(first.Password, password) || first.Password == second.Password {
		t.Fatalf("stored passwords %q and %q", first.Password, second.Password)
	}
}

func TestChangingThePasswordSignsOutOtherDevices(t *testing.T) {
	w := newWorld(t)
	first := w.registered("Hawke")
	second := w.signIn("Hawke", password)
	if _, err := w.store.ChangePassword(first.PlayerID, otherPassword, "new password", second.Secret); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("change with a wrong current password: %v", err)
	}
	if _, err := w.store.ChangePassword(first.PlayerID, password, "short", second.Secret); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("change to a short password: %v", err)
	}
	if _, err := w.store.ChangePassword(first.PlayerID, password, otherPassword, second.Secret); err != nil {
		t.Fatal(err)
	}
	w.expectHeld(first, false)
	w.expectHeld(second, true)
	if _, err := w.store.SignIn("Hawke", password); !errors.Is(err, ErrWrongCredentials) {
		t.Fatalf("old password still signs in: %v", err)
	}
	w.signIn("Hawke", otherPassword)
}

func TestGuestHasNoPasswordToChange(t *testing.T) {
	w := newWorld(t)
	guest := w.guest()
	if _, err := w.store.ChangePassword(guest.PlayerID, "", otherPassword, guest.Secret); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("guest password change: %v", err)
	}
	if err := w.store.SignOut(guest.PlayerID, guest.Secret); !errors.Is(err, ErrNoLogin) {
		t.Fatalf("guest sign out: %v", err)
	}
	w.expectHeld(guest, true)
}

func TestSignOutRevokesOnlyThatDevice(t *testing.T) {
	w := newWorld(t)
	first := w.registered("Hawke")
	second := w.signIn("Hawke", password)
	if err := w.store.SignOut(first.PlayerID, first.Secret); err != nil {
		t.Fatal(err)
	}
	w.expectHeld(first, false)
	w.expectHeld(second, true)
	record, err := w.store.Find(first.PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	if record.DeviceCount() != 1 {
		t.Fatalf("%d devices signed in, want 1", record.DeviceCount())
	}
}

func TestOldestDeviceGivesWayToANewOne(t *testing.T) {
	w := newWorld(t)
	w.registered("Hawke")
	var devices []Credentials
	for range MaxSignedInDevices + 1 {
		w.clock = w.clock.Add(time.Minute)
		devices = append(devices, w.signIn("Hawke", password))
	}
	w.expectHeld(devices[0], false)
	w.expectHeld(devices[1], true)
	w.expectHeld(devices[MaxSignedInDevices], true)
}

func TestDeletingAnAccountFreesItsLogin(t *testing.T) {
	w := newWorld(t)
	owner := w.registered("Hawke")
	if err := w.store.ConfirmOwner(owner.PlayerID, otherPassword); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password confirmed the owner: %v", err)
	}
	if err := w.store.ConfirmOwner(owner.PlayerID, password); err != nil {
		t.Fatal(err)
	}
	if err := w.store.Delete(owner.PlayerID); err != nil {
		t.Fatal(err)
	}
	if _, err := w.store.Find(owner.PlayerID); !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("deleted account still found: %v", err)
	}
	if _, err := w.store.SignIn("Hawke", password); !errors.Is(err, ErrWrongCredentials) {
		t.Fatalf("deleted account signs in: %v", err)
	}
	if _, err := w.store.SetLogin(w.guest().PlayerID, "Hawke", password); err != nil {
		t.Fatalf("login of a deleted account stayed taken: %v", err)
	}
}

func TestGuestConfirmsOwnershipWithoutAPassword(t *testing.T) {
	w := newWorld(t)
	if err := w.store.ConfirmOwner(w.guest().PlayerID, ""); err != nil {
		t.Fatal(err)
	}
}

func TestResetGivesATemporaryPasswordAndSignsEveryDeviceOut(t *testing.T) {
	w := newWorld(t)
	first := w.registered("Hawke")
	second := w.signIn("Hawke", password)
	reset, err := w.store.ResetPassword("hawke")
	if err != nil {
		t.Fatal(err)
	}
	if reset.PlayerID != first.PlayerID || reset.Login != "Hawke" || !ValidPassword(reset.Password) {
		t.Fatalf("reset = %+v", reset)
	}
	w.expectHeld(first, false)
	w.expectHeld(second, false)
	if _, err := w.store.SignIn("Hawke", password); !errors.Is(err, ErrWrongCredentials) {
		t.Fatalf("forgotten password still signs in: %v", err)
	}
	w.expectHeld(w.signIn("Hawke", reset.Password), true)
	if _, err := w.store.ResetPassword("Varric"); !errors.Is(err, ErrUnknownLogin) {
		t.Fatalf("reset of an unknown login: %v", err)
	}
}

func TestPlayerWithALoginIsNotAGuest(t *testing.T) {
	w := newWorld(t)
	w.guest()
	w.registered("Hawke")
	guests, err := w.store.Guests()
	if err != nil {
		t.Fatal(err)
	}
	if len(guests) != 1 || guests[0].HasLogin() {
		t.Fatalf("guests = %+v", guests)
	}
}
