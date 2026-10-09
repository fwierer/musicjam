package room

import (
	"math/rand"
	"time"
)

// Room seed and allowed scope
const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var seededRand *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

// Class member
type Member struct {
	isHost   bool
	userName string
}

// class Room
type Room struct {
	roomCode    string
	host        Member
	roomMembers map[Member]int
}

// Constructor for Room that assignes a new room code
func NewRoom() *Room {
	return &Room{roomCode: GenerateRandomRoomNo(6)}
}

// geter to make the roomcode inaccessable form the outside
func (room *Room) Code() string {
	return room.roomCode
}

// Generate seeded 6 digit code
func GenerateRandomRoomNo(length int) string {
	byteArr := make([]byte, length)
	for i := range byteArr {
		byteArr[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(byteArr)
}
