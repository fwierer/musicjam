package room

import (
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"unicode/utf8"
)

// Allowed characters for room codes
const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

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

type Room struct {
	roomCode           string
	hostID             string
	members            map[string]Member // key: user ID
	Queue              []Song
	Genre              string
	Requests           []*SongRequest
	ProcessingRequests bool
	nextRequest        int
}

// AddMember adds a user to the room, or renames them if they already joined
func (room *Room) AddMember(userID, name string) Member {
	m, ok := room.members[userID]
	if !ok {
		m = Member{ID: crand.Text(), UserID: userID, IsHost: room.IsHost(userID)}
	}
	m.Name = name
	room.members[userID] = m
	return m
}

// RemoveMember removes the member with the given public ID and returns them.
// The host can't be removed, a room always needs its host.
func (room *Room) RemoveMember(memberID string) (Member, error) {
	for userID, m := range room.members {
		if m.ID != memberID {
			continue
		}
		if m.IsHost {
			return Member{}, ErrRemoveHost
		}
		delete(room.members, userID)
		return m, nil
	}
	return Member{}, ErrMemberNotFound
}

// MemberList returns the members, host first, then by name
func (room *Room) MemberList() []Member {
	members := make([]Member, 0, len(room.members))
	for _, m := range room.members {
		members = append(members, m)
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].IsHost != members[j].IsHost {
			return members[i].IsHost
		}
		return members[i].Name < members[j].Name
	})
	return members
}

func (room *Room) IsHost(userID string) bool {
	return room.hostID == userID
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

// Constructor for Room that assignes a new room code.
// codeTaken reports whether a code is already in use, so the new code is unique.
func NewRoom(hostUserID, hostName string, codeTaken func(string) bool) *Room {
	code := GenerateRandomRoomNo(6)
	for codeTaken(code) {
		code = GenerateRandomRoomNo(6)
	}
	room := &Room{
		roomCode: code,
		hostID:   hostUserID,
		members:  map[string]Member{},
		Queue:    []Song{},
		Genre:    "Any",
		Requests: []*SongRequest{},
	}
	room.AddMember(hostUserID, hostName)
	return room
}

// geter to make the roomcode inaccessable form the outside
func (room *Room) Code() string {
	return room.roomCode
}

// String formats the room for the terminal, host first, then members by name
func (room *Room) String() string {
	members := room.MemberList()
	nameWidth := len("(no name)")
	for _, m := range members {
		nameWidth = max(nameWidth, len(m.Name))
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Room %s | %d members | %d songs in queue\n", room.roomCode, len(members), len(room.Queue))
	for _, m := range members {
		role := ""
		if m.IsHost {
			role = "host"
		}
		name := m.Name
		if name == "" {
			name = "(no name)"
		}
		fmt.Fprintf(&b, "    %-4s  %-*s  %s\n", role, nameWidth, name, m.ID)
	}
	for i, song := range room.Queue {
		fmt.Fprintf(&b, "    %2d. %s - %s\n", i+1, song.Title, song.Channel)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// Generate a random code of the given length (math/rand/v2 is safe for concurrent use)
func GenerateRandomRoomNo(length int) string {
	byteArr := make([]byte, length)
	for i := range byteArr {
		byteArr[i] = charset[rand.IntN(len(charset))]
	}
	return string(byteArr)
}
