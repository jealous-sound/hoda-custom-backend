package account

import (
	"crypto/subtle"
	"errors"
	"slices"
	"time"
)

const MaxSignedInDevices = 8

var (
	ErrInvalidLogin     = errors.New("login must be 3-24 letters, digits, '.', '_' or '-', starting with a letter or digit")
	ErrInvalidPassword  = errors.New("password must be 8-64 characters")
	ErrLoginTaken       = errors.New("login belongs to another player")
	ErrHasLogin         = errors.New("account already has a login")
	ErrNoLogin          = errors.New("account has no login")
	ErrWrongPassword    = errors.New("wrong password")
	ErrWrongCredentials = errors.New("wrong login or password")
	ErrUnknownLogin     = errors.New("no account has this login")
)

type Reset struct {
	PlayerID string
	Login    string
	Password string
}

func (a Account) holds(secret string) bool {
	candidate := []byte(hashSecret(secret))
	held := subtle.ConstantTimeCompare([]byte(a.SecretHash), candidate)
	for _, device := range a.Devices {
		held |= subtle.ConstantTimeCompare([]byte(device.SecretHash), candidate)
	}
	return held == 1
}

func (a Account) DeviceCount() int {
	count := len(a.Devices)
	if a.SecretHash != "" {
		count++
	}
	return count
}

func (a *Account) admit(secret string, at time.Time) {
	a.Devices = append(a.Devices, Device{SecretHash: hashSecret(secret), SignedInAt: at})
	if extra := len(a.Devices) - MaxSignedInDevices; extra > 0 {
		slices.SortStableFunc(a.Devices, func(x, y Device) int { return x.SignedInAt.Compare(y.SignedInAt) })
		a.Devices = slices.Clone(a.Devices[extra:])
	}
}

func (a *Account) keepDevices(keep func(secretHash string) bool) {
	if !keep(a.SecretHash) {
		a.SecretHash = ""
	}
	a.Devices = slices.DeleteFunc(a.Devices, func(device Device) bool { return !keep(device.SecretHash) })
}

func (a *Account) revoke(secret string) {
	revoked := hashSecret(secret)
	a.keepDevices(func(secretHash string) bool { return secretHash != revoked })
}

func (a *Account) keepOnly(secret string) {
	kept := hashSecret(secret)
	a.keepDevices(func(secretHash string) bool { return secretHash == kept })
}

func (a *Account) revokeEveryDevice() {
	a.keepDevices(func(string) bool { return false })
}

func (s *Store) Find(playerID string) (Account, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.read(playerID)
}

func (s *Store) change(playerID string, change func(record *Account) error) (Account, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	record, err := s.read(playerID)
	if err != nil {
		return Account{}, err
	}
	if err := change(&record); err != nil {
		return Account{}, err
	}
	return record, s.write(record)
}

func (s *Store) SetLogin(playerID, login, password string) (Account, error) {
	if !ValidLogin(login) {
		return Account{}, ErrInvalidLogin
	}
	if !ValidPassword(password) {
		return Account{}, ErrInvalidPassword
	}
	hashed, err := hashPassword(password, s.passwordIterations)
	if err != nil {
		return Account{}, err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	record, err := s.read(playerID)
	if err != nil {
		return Account{}, err
	}
	if record.HasLogin() {
		return Account{}, ErrHasLogin
	}
	key := LoginKey(login)
	if _, taken := s.owners[key]; taken {
		return Account{}, ErrLoginTaken
	}
	record.Login, record.Password = login, hashed
	if err := s.write(record); err != nil {
		return Account{}, err
	}
	s.owners[key] = playerID
	return record, nil
}

func (s *Store) owner(login string) (Account, bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	playerID, known := s.owners[LoginKey(login)]
	if !known {
		return Account{}, false, nil
	}
	record, err := s.read(playerID)
	if errors.Is(err, ErrNotRegistered) {
		return Account{}, false, nil
	}
	return record, err == nil, err
}

func (s *Store) SignIn(login, password string) (Credentials, error) {
	record, known, err := s.owner(login)
	if err != nil {
		return Credentials{}, err
	}
	stored := record.Password
	if !known {
		if stored, err = s.decoy(); err != nil {
			return Credentials{}, err
		}
	}
	if matches := passwordMatches(stored, password); !known || !matches {
		return Credentials{}, ErrWrongCredentials
	}
	secret, err := randomHex(secretBytes)
	if err != nil {
		return Credentials{}, err
	}
	_, err = s.change(record.PlayerID, func(current *Account) error {
		if current.Password != stored {
			return ErrWrongCredentials
		}
		current.admit(secret, s.now().UTC())
		return nil
	})
	if errors.Is(err, ErrNotRegistered) {
		err = ErrWrongCredentials
	}
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{PlayerID: record.PlayerID, Secret: secret}, nil
}

func (s *Store) ConfirmOwner(playerID, password string) error {
	record, err := s.Find(playerID)
	if err != nil {
		return err
	}
	if record.HasLogin() && !passwordMatches(record.Password, password) {
		return ErrWrongPassword
	}
	return nil
}

func (s *Store) ChangePassword(playerID, current, next, keptSecret string) (Account, error) {
	if !ValidPassword(next) {
		return Account{}, ErrInvalidPassword
	}
	record, err := s.Find(playerID)
	if err != nil {
		return Account{}, err
	}
	if !record.HasLogin() {
		return Account{}, ErrNoLogin
	}
	if !passwordMatches(record.Password, current) {
		return Account{}, ErrWrongPassword
	}
	hashed, err := hashPassword(next, s.passwordIterations)
	if err != nil {
		return Account{}, err
	}
	return s.change(playerID, func(latest *Account) error {
		if latest.Password != record.Password {
			return ErrWrongPassword
		}
		latest.Password = hashed
		latest.keepOnly(keptSecret)
		return nil
	})
}

func (s *Store) SignOut(playerID, secret string) error {
	_, err := s.change(playerID, func(record *Account) error {
		if !record.HasLogin() {
			return ErrNoLogin
		}
		record.revoke(secret)
		return nil
	})
	return err
}

func (s *Store) Delete(playerID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	record, err := s.read(playerID)
	if err != nil {
		return err
	}
	return s.remove(record)
}

func (s *Store) ResetPassword(login string) (Reset, error) {
	owner, known, err := s.owner(login)
	if err != nil {
		return Reset{}, err
	}
	if !known {
		return Reset{}, ErrUnknownLogin
	}
	password, err := randomHex(temporaryPasswordBytes)
	if err != nil {
		return Reset{}, err
	}
	hashed, err := hashPassword(password, s.passwordIterations)
	if err != nil {
		return Reset{}, err
	}
	record, err := s.change(owner.PlayerID, func(record *Account) error {
		if !record.HasLogin() {
			return ErrUnknownLogin
		}
		record.Password = hashed
		record.revokeEveryDevice()
		return nil
	})
	if errors.Is(err, ErrNotRegistered) {
		err = ErrUnknownLogin
	}
	return Reset{PlayerID: record.PlayerID, Login: record.Login, Password: password}, err
}
