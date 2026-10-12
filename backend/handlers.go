package main

import (
	"crypto/rand"
	"encoding/json"
	"log"
	"net/http"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

// handlers
func registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/youtube/search", handleYouTubeSearch)
	mux.HandleFunc("/api/room/create", createNewRoom)
	mux.HandleFunc("/api/room/join", joinRoom)
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

// creates new room
func createNewRoom(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	userID := getUserID(w, r)
	newRoom := room.NewRoom(
		room.Member{
			ID:   userID,
			Name: r.URL.Query().Get("name")},
		func(code string) bool { _, taken := rooms[code]; return taken })
	rooms[newRoom.Code()] = newRoom
	log.Printf("room created\n%s", newRoom)
	sessions[userID] = &Session{Name: r.URL.Query().Get("name"), RoomCode: newRoom.Code(), IsHost: true}
	json.NewEncoder(w).Encode(map[string]any{"code": newRoom.Code(), "isHost": true})
}

func joinRoom(w http.ResponseWriter, r *http.Request) {
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
	rm.AddMember(room.Member{ID: userID, Name: r.URL.Query().Get("name")})
	log.Printf("%q joined\n%s", r.URL.Query().Get("name"), rm)
	sessions[userID] = &Session{Name: r.URL.Query().Get("name"), RoomCode: code, IsHost: rm.IsHost(userID)}
	json.NewEncoder(w).Encode(map[string]any{"code": code, "isHost": rm.IsHost(userID)})
}

// returns the session of the current user
func getSession(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("userId")
	if err != nil {
		http.Error(w, "no session", http.StatusUnauthorized)
		return
	}
	mu.Lock()
	s, ok := sessions[c.Value]
	mu.Unlock()
	if !ok {
		http.Error(w, "no session", http.StatusUnauthorized)
		return
	}
	json.NewEncoder(w).Encode(s)
}
