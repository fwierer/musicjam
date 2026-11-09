package room

import (
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Room struct {
	mutex              sync.RWMutex
	roomCode           string
	Name               string
	hostID             string
	members            map[string]Member // key: user ID
	Queue              []Song
	Genre              string
	Requests           []*SongRequest
	ProcessingRequests bool
	nextRequest        int
	UpdatedAt          int64
}

// Constructor for Room that assignes a new room code.
// codeTaken reports whether a code is already in use, so the new code is unique.
func NewRoom(hostUserID, hostName string, codeTaken func(string) bool) *Room {
	code := GenerateUUIDv4()
	for codeTaken(code) {
		code = GenerateUUIDv4()
	}
	room := &Room{
		roomCode: code,
		Name:     hostName + "'s Jam",
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

// Member returns the member with the given secret user ID
func (room *Room) Member(userID string) (Member, bool) {
	m, ok := room.members[userID]
	return m, ok
}

// Members returns a copy of the room's members keyed by secret user ID
func (room *Room) Members() map[string]Member {
	out := make(map[string]Member, len(room.members))
	for userID, m := range room.members {
		out[userID] = m
	}
	return out
}

// Save writes the current room state (including members, queue, and requests) to its JSON file
func (room *Room) Save() {
	saveToJsonFile(room)
}

// AddMember adds a user to the room, or renames them if they already joined
func (room *Room) AddMember(userID, name string) Member {
	m, ok := room.members[userID]
	if !ok {
		m = Member{ID: GenerateUUIDv4(), UserID: userID, IsHost: room.IsHost(userID)}
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

// GenerateUUIDv4 generates a random RFC 4122 UUID v4 string using crypto/rand
func GenerateUUIDv4() string {
	var b [16]byte
	crand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
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

// DataDir is the directory where room JSON files are stored
var DataDir = "../database"

type songRequestJSON struct {
	ID          string `json:"id"`
	Song        Song   `json:"song"`
	UserID      string `json:"user_id,omitempty"`
	RequestedBy string `json:"requestedBy"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Queued      bool   `json:"queued"`
}

type roomJSON struct {
	RoomCode    string            `json:"room_code"`
	Name        string            `json:"name,omitempty"`
	HostID      string            `json:"host_id"`
	Members     map[string]Member `json:"members"`
	Queue       []Song            `json:"queue"`
	Genre       string            `json:"genre"`
	Requests    []songRequestJSON `json:"requests"`
	NextRequest int               `json:"next_request"`
	UpdatedAt   int64             `json:"updated_at,omitempty"`
}

func saveToJsonFile(room *Room) {
	room.mutex.Lock() // Restrict other goroutines from modifying the room while saving the data
	room.UpdatedAt = time.Now().UnixNano()
	requests := make([]songRequestJSON, 0, len(room.Requests))
	for _, r := range room.Requests {
		requests = append(requests, songRequestJSON{
			ID:          r.ID,
			Song:        r.Song,
			UserID:      r.UserID,
			RequestedBy: r.RequestedBy,
			Status:      r.Status,
			Reason:      r.Reason,
			Queued:      r.Queued,
		})
	}
	roomData := roomJSON{
		RoomCode:    room.roomCode,
		Name:        room.Name,
		HostID:      room.hostID,
		Members:     room.members,
		Queue:       room.Queue,
		Genre:       room.Genre,
		Requests:    requests,
		NextRequest: room.nextRequest,
		UpdatedAt:   room.UpdatedAt,
	}
	room.mutex.Unlock()

	filename := filepath.Join(DataDir, "room_"+room.roomCode+".json")
	tmpfilename := filename + ".tmp"
	fileData, err := json.MarshalIndent(roomData, "", "  ")
	if err != nil {
		fmt.Println("Error marshalling room data:", err)
		return
	}
	// Write to temp file then rename so we prevent data loss
	if err := os.WriteFile(tmpfilename, fileData, 0644); err != nil {
		return
	}
	_ = os.Rename(tmpfilename, filename)
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
		// Only load room_*.json files
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "room_") || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(dbDir, entry.Name())
		fileData, err := os.ReadFile(filePath)
		if err != nil {
			continue // skip files that fail to read
		}

		// Unmarshal into your JSON representation struct
		var data roomJSON
		if err := json.Unmarshal(fileData, &data); err != nil || data.RoomCode == "" {
			continue // skip corrupted files
		}

		if data.Members == nil {
			data.Members = map[string]Member{}
		}
		// Restore each member's secret UserID from the JSON map key
		for userID, m := range data.Members {
			m.UserID = userID
			data.Members[userID] = m
		}
		if data.Queue == nil {
			data.Queue = []Song{}
		}
		requests := make([]*SongRequest, 0, len(data.Requests))
		for _, r := range data.Requests {
			requests = append(requests, &SongRequest{
				ID:          r.ID,
				Song:        r.Song,
				UserID:      r.UserID,
				RequestedBy: r.RequestedBy,
				Status:      r.Status,
				Reason:      r.Reason,
				Queued:      r.Queued,
			})
		}
		if data.NextRequest > len(requests) {
			data.NextRequest = len(requests)
		}
		if data.Name == "" {
			if host, ok := data.Members[data.HostID]; ok && host.Name != "" {
				data.Name = host.Name + "'s Jam"
			}
		}
		if data.UpdatedAt == 0 {
			if info, err := entry.Info(); err == nil {
				data.UpdatedAt = info.ModTime().UnixNano()
			}
		}

		// Reconstruct the Room struct
		room := &Room{
			roomCode:    data.RoomCode,
			Name:        data.Name,
			hostID:      data.HostID,
			members:     data.Members,
			Queue:       data.Queue,
			Genre:       data.Genre,
			Requests:    requests,
			nextRequest: data.NextRequest,
			UpdatedAt:   data.UpdatedAt,
		}

		// Store it in active rooms map using the room code as key
		activeRooms[room.roomCode] = room
	}

	return activeRooms
}
