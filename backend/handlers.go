package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

// handlers
func registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/youtube/search", handleYouTubeSearch)
	mux.HandleFunc("/api/room/create", createNewRoom)
	mux.HandleFunc("/api/room/join", joinRoom)
	mux.HandleFunc("/api/room/queue", handleQueue)
	mux.HandleFunc("/api/genres", getGenres)
	mux.HandleFunc("/api/room/requests", handleSongRequests)
	mux.HandleFunc("/api/room/requests/decision", decideSongRequest)
	mux.HandleFunc("/api/room/getRoomMembers", getRoomMembers)
	mux.HandleFunc("/api/room/removeRoomMember", removeRoomMember)
	mux.HandleFunc("/api/session", getSession)
	mux.Handle("/", http.FileServer(http.Dir("../frontend")))
}

// getUserID returns the user's ID from their cookie, or creates a new user.
// New IDs come from crypto/rand so they can't be guessed, and are checked against
// existing sessions so they are unique. Caller must hold mu.
func getUserID(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie("userId"); err == nil {
		return c.Value
	}
	id := rand.Text()
	for sessions[id] != nil {
		id = rand.Text()
	}
	http.SetCookie(w, &http.Cookie{Name: "userId", Value: id, Path: "/", HttpOnly: true})
	return id
}

// caller is the user behind a request, with their session and room
type caller struct {
	userID  string
	session *Session
	room    *room.Room
}

// currentCaller finds the caller's session and room from the userId cookie.
// If there is none it writes the error response and returns false. Caller must hold mu.
func currentCaller(w http.ResponseWriter, r *http.Request) (caller, bool) {
	c, err := r.Cookie("userId")
	if err != nil {
		http.Error(w, "no session", http.StatusUnauthorized)
		return caller{}, false
	}
	s, ok := sessions[c.Value]
	if !ok {
		http.Error(w, "no session", http.StatusUnauthorized)
		return caller{}, false
	}
	rm, ok := rooms[s.RoomCode]
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return caller{}, false
	}
	return caller{userID: c.Value, session: s, room: rm}, true
}

// currentHost is currentCaller for actions only the host of the room may take
func currentHost(w http.ResponseWriter, r *http.Request) (caller, bool) {
	c, ok := currentCaller(w, r)
	if ok && !c.room.IsHost(c.userID) {
		http.Error(w, "only the host can do this", http.StatusForbidden)
		return caller{}, false
	}
	return c, ok
}

// memberName reads the name every member must enter before creating or joining a room.
// If it is missing or invalid it writes the error response and returns false.
func memberName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name, err := room.CleanName(r.URL.Query().Get("name"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return "", false
	}
	return name, true
}

// creates new room
func createNewRoom(w http.ResponseWriter, r *http.Request) {
	genre, err := room.CleanGenre(r.URL.Query().Get("genre"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name, ok := memberName(w, r)
	if !ok {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	userID := getUserID(w, r)
	newRoom := room.NewRoom(userID, name,
		func(code string) bool { _, taken := rooms[code]; return taken })
	rooms[newRoom.Code()] = newRoom
	newRoom.Genre = genre
	log.Printf("room created\n%s", newRoom)
	sessions[userID] = &Session{Name: name, RoomCode: newRoom.Code(), IsHost: true}
	json.NewEncoder(w).Encode(map[string]any{"code": newRoom.Code(), "isHost": true})
}

func joinRoom(w http.ResponseWriter, r *http.Request) {
	name, ok := memberName(w, r)
	if !ok {
		return
	}
	code := r.URL.Query().Get("code")
	mu.Lock()
	defer mu.Unlock()
	rm, ok := rooms[code]
	// if room not found throw error
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	// get user from current session if there is none than create one
	userID := getUserID(w, r)
	rm.AddMember(userID, name)
	log.Printf("%q joined\n%s", name, rm)
	sessions[userID] = &Session{Name: name, RoomCode: code, IsHost: rm.IsHost(userID)}
	json.NewEncoder(w).Encode(map[string]any{"code": code, "isHost": rm.IsHost(userID)})
}

func getRoomMembers(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	mu.Lock()
	defer mu.Unlock()
	curentRoom, ok := rooms[code]
	// if room not found throw error
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	// A list instead of the map, whose keys are the members' secret user IDs
	json.NewEncoder(w).Encode(curentRoom.MemberList())
}

// removeRoomMember lets the host kick a member out of their room.
// Expects DELETE with {"memberId": "..."} and returns the members that are left.
func removeRoomMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", "DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	host, ok := currentHost(w, r)
	if !ok {
		return
	}
	var body struct {
		MemberID string `json:"memberId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.MemberID == "" {
		http.Error(w, "missing member ID", http.StatusBadRequest)
		return
	}
	removed, err := host.room.RemoveMember(body.MemberID)
	if errors.Is(err, room.ErrMemberNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Ending their session locks them out of the room. Only if it still belongs to this room,
	// they may have joined another one since.
	if s := sessions[removed.UserID]; s != nil && s.RoomCode == host.room.Code() {
		delete(sessions, removed.UserID)
	}
	releaseRoomRequests(host.room)
	log.Printf("%q was removed\n%s", removed.Name, host.room)
	json.NewEncoder(w).Encode(host.room.MemberList())
}

// returns the session of the current user
func getSession(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("userId")
	if err != nil {
		http.Error(w, "no session", http.StatusUnauthorized)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	currentSession, ok := sessions[c.Value]
	if !ok {
		http.Error(w, "no session", http.StatusUnauthorized)
		return
	}
	rm := rooms[currentSession.RoomCode]
	if rm == nil {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"name": currentSession.Name, "roomCode": currentSession.RoomCode, "isHost": currentSession.IsHost, "genre": rm.Genre})
}

func handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		submitSong(w, r, true)
		return
	}
	mu.Lock()
	defer mu.Unlock()

	c, ok := currentCaller(w, r)
	if !ok {
		return
	}
	rm := c.room

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(rm.Queue)
	case http.MethodDelete:
		if !rm.IsHost(c.userID) {
			http.Error(w, "only the host can advance playback", http.StatusForbidden)
			return
		}
		var finished struct {
			SongID string `json:"songId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&finished); err != nil || finished.SongID == "" {
			http.Error(w, "missing song ID", http.StatusBadRequest)
			return
		}
		// A delayed or repeated end event must not remove the next song.
		if len(rm.Queue) == 0 || rm.Queue[0].ID != finished.SongID {
			http.Error(w, "current song has changed", http.StatusConflict)
			return
		}
		rm.Queue = rm.Queue[1:]
		log.Printf("song finished\n%s", rm)
		json.NewEncoder(w).Encode(rm.Queue)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
