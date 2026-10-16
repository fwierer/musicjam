package room

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
)

// Allowed characters for room codes
const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// Song in a room's queue
type Song struct {
	ID      string `json:"id"`
	VideoID string `json:"videoId"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
}

// room/room.go
type Member struct {
	ID     string
	Name   string
	IsHost bool
}

type Room struct {
	roomCode string
	hostID   string
	Members  map[string]Member // key: user ID
	Queue    []Song
}

func (room *Room) AddMember(m Member) {
	room.Members[m.ID] = m
}
func (room *Room) IsHost(userID string) bool {
	return room.hostID == userID
}

// Constructor for Room that assignes a new room code.
// codeTaken reports whether a code is already in use, so the new code is unique.
func NewRoom(host Member, codeTaken func(string) bool) *Room {
	host.IsHost = true
	code := GenerateRandomRoomNo(6)
	for codeTaken(code) {
		code = GenerateRandomRoomNo(6)
	}
	return &Room{
		roomCode: code,
		hostID:   host.ID,
		Members:  map[string]Member{host.ID: host},
		Queue:    []Song{},
	}
}

// geter to make the roomcode inaccessable form the outside
func (room *Room) Code() string {
	return room.roomCode
}

// String formats the room for the terminal, host first, then members by name
func (room *Room) String() string {
	members := make([]Member, 0, len(room.Members))
	nameWidth := len("(no name)")
	for _, m := range room.Members {
		members = append(members, m)
		nameWidth = max(nameWidth, len(m.Name))
	}
	sort.Slice(members, func(i, j int) bool {
		if members[i].IsHost != members[j].IsHost {
			return members[i].IsHost
		}
		return members[i].Name < members[j].Name
	})

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
