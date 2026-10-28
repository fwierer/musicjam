package room

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Longest name (in characters) a member can pick
const MaxNameLength = 30

var (
	ErrNameRequired   = errors.New("please enter a name")
	ErrNameTooLong    = fmt.Errorf("name can be at most %d characters", MaxNameLength)
	ErrMemberNotFound = errors.New("member not found")
	ErrRemoveHost     = errors.New("the host can't be removed")
)

// Song in a room's queue
type Song struct {
	ID      string `json:"id"`
	VideoID string `json:"videoId"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
}

// Member of a room. UserID is the secret from the userId cookie, so it is never sent to clients.
// Clients point at a member with the public ID instead, e.g. when the host removes them.
type Member struct {
	ID     string `json:"id"`
	UserID string `json:"-"`
	Name   string `json:"name"`
	IsHost bool   `json:"isHost"`
}

// CleanName trims the spaces around a member name and checks that it is valid
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ErrNameRequired
	}
	if utf8.RuneCountInString(name) > MaxNameLength {
		return "", ErrNameTooLong
	}
	return name, nil
}
