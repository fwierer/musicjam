package room

import (
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Allowed characters for room codes
const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

type Room struct {
	mutex              sync.RWMutex
	roomCode           string
	hostID             string
	members            map[string]Member // key: user ID
	Queue              []Song
	Genre              string
	Requests           []*SongRequest
	ProcessingRequests bool
	nextRequest        int
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
	saveToJsonFile(room)
	return room
}

// Code is a getter to make the roomcode inaccessible from the outside
func (room *Room) Code() string {
	return room.roomCode
}

// AddMember adds a user to the room, or renames them if they already joined
func (room *Room) AddMember(userID, name string) Member {
	m, ok := room.members[userID]
	if !ok {
		m = Member{ID: crand.Text(), UserID: userID, IsHost: room.IsHost(userID)}
	}
	m.Name = name
	room.members[userID] = m
	saveToJsonFile(room)
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
		saveToJsonFile(room)
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

// GenerateRandomRoomNo generates a random code of the given length (math/rand/v2 is safe for concurrent use)
func GenerateRandomRoomNo(length int) string {
	byteArr := make([]byte, length)
	for i := range byteArr {
		byteArr[i] = charset[rand.IntN(len(charset))]
	}
	return string(byteArr)
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

type roomJSON struct {
	RoomCode           string            `json:"room_code"`
	HostID             string            `json:"host_id"`
	Members            map[string]Member `json:"members"`
	Queue              []Song            `json:"queue"`
	Genre              string            `json:"genre"`
	ProcessingRequests bool              `json:"processing_requests"`
	NextRequest        int               `json:"next_request"`
}

func saveToJsonFile(room *Room) {
	room.mutex.Lock() // Restrict other goroutines from modifying the room while saving the data
	roomData := roomJSON{
		RoomCode:           room.roomCode,
		HostID:             room.hostID,
		Members:            room.members,
		Queue:              room.Queue,
		Genre:              room.Genre,
		ProcessingRequests: room.ProcessingRequests,
		NextRequest:        room.nextRequest,
	}
	room.mutex.Unlock()
	filename := "../database/room_" + room.roomCode + ".json"
	tmpfilename := "../database/room_" + room.roomCode + ".json.tmp"
	fileData, err := json.MarshalIndent(roomData, "", "  ")
	if err != nil {
		fmt.Println("Error marshalling room data:", err)
		return
	}
	// Write to temp file then rename so we prenent data loss
	os.WriteFile(tmpfilename, fileData, 0644)
	os.Rename(tmpfilename, filename)
}

func LoadExistingRoomsFromDatabase(dbDir string) map[string]*Room {
	activeRooms := make(map[string]*Room)

	// Read all files in the database directory
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		// If the directory doesn't exist yet, it's fine (no rooms to load)
		if os.IsNotExist(err) {
			return activeRooms
		}
		return nil
	}

	for _, entry := range entries {
		// Look for JSON files
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dbDir, entry.Name())
		fileData, err := os.ReadFile(filePath)
		if err != nil {
			continue // skip files that fail to read
		}

		// Unmarshal into your JSON representation struct
		var data roomJSON
		if err := json.Unmarshal(fileData, &data); err != nil {
			continue // skip corrupted files
		}

		// Reconstruct the Room struct
		room := &Room{
			roomCode:           data.RoomCode,
			hostID:             data.HostID,
			members:            data.Members,
			Queue:              data.Queue,
			Genre:              data.Genre,
			ProcessingRequests: data.ProcessingRequests,
			nextRequest:        data.NextRequest,
		}

		// Store it in  active rooms map using the room code as key
		activeRooms[room.roomCode] = room
	}

	return activeRooms
}
