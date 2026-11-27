package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

const cookieMaxAge = 24 * 60 * 60 // 24 hours in seconds

var (
	classifier     songClassifier = newLLMClient()
	verifySong                    = lookupYouTubeSong
	videoIDPattern                = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
)

func registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/youtube/search", handleYouTubeSearch)
	mux.HandleFunc("/api/player/fallback", handlePlayerFallback)
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/api/room/create", createNewRoom)
	mux.HandleFunc("/api/room/join", joinRoom)
	mux.HandleFunc("/api/room/queue", handleQueue)
	mux.HandleFunc("/api/room/skip", skipSong)
	mux.HandleFunc("/api/genres", getGenres)
	mux.HandleFunc("/api/room/requests", handleSongRequests)
	mux.HandleFunc("/api/room/requests/decision", decideSongRequest)
	mux.HandleFunc("/api/room/getRoomMembers", getRoomMembers)
	mux.HandleFunc("/api/room/removeRoomMember", removeRoomMember)
	mux.HandleFunc("/api/session", getSession)
	frontendDir := os.Getenv("FRONTEND_DIR")
	if frontendDir == "" {
		frontendDir = "../frontend"
	}
	mux.Handle("/", http.FileServer(http.Dir(frontendDir)))
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// getUserID returns the user's ID from their cookie, or creates a new user.
// New IDs come from crypto/rand so they can't be guessed, and are checked against
// existing sessions so they are unique. Caller must hold mu.
func getUserID(w http.ResponseWriter, r *http.Request) string {
	id := ""
	if c, err := r.Cookie("userId"); err == nil && c.Value != "" {
		id = c.Value
	} else {
		id = room.GenerateUUIDv4()
		for sessions[id] != nil {
			id = room.GenerateUUIDv4()
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "userId",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   cookieMaxAge,
	})
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
	roomName, err := room.CleanRoomName(r.URL.Query().Get("roomName"), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	userID := getUserID(w, r)
	newRoom := room.NewRoom(userID, name,
		func(code string) bool { _, taken := rooms[code]; return taken })
	newRoom.Name = roomName
	newRoom.Genre = genre
	newRoom.Save()
	rooms[newRoom.Code()] = newRoom
	log.Printf("room created\n%s", newRoom)
	sessions[userID] = &Session{Name: name, RoomCode: newRoom.Code(), IsHost: true}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"code": newRoom.Code(), "name": newRoom.Name, "isHost": true})
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
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	userID := getUserID(w, r)
	rm.AddMember(userID, name)
	log.Printf("%q joined\n%s", name, rm)
	sessions[userID] = &Session{Name: name, RoomCode: code, IsHost: rm.IsHost(userID)}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"code": code, "name": rm.Name, "isHost": rm.IsHost(userID)})
}

func getRoomMembers(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	mu.Lock()
	defer mu.Unlock()
	currentRoom, ok := rooms[code]
	if !ok {
		http.Error(w, "room not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(currentRoom.MemberList())
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
	if s := sessions[removed.UserID]; s != nil && s.RoomCode == host.room.Code() {
		delete(sessions, removed.UserID)
	}
	releaseRoomRequests(host.room)
	host.room.Save()
	log.Printf("%q was removed\n%s", removed.Name, host.room)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(host.room.MemberList())
}

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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"name":       currentSession.Name,
		"roomCode":   currentSession.RoomCode,
		"roomName":   rm.Name,
		"isHost":     currentSession.IsHost,
		"genre":      rm.Genre,
		"joinOrigin": getJoinOrigin(r),
	})
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
		w.Header().Set("Content-Type", "application/json")
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
		if _, err := rm.SkipCurrentSong(finished.SongID); err != nil {
			http.Error(w, "current song has changed", http.StatusConflict)
			return
		}
		log.Printf("song finished\n%s", rm)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rm.Queue)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// skipSong lets ONLY the room host skip the currently playing song (manually or when
// YouTube blocks embedded playback due to copyright / embed restrictions).
func skipSong(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		SongID string `json:"songId"`
		Reason string `json:"reason"`
	}
	if r.Body != nil {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if err := dec.Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			http.Error(w, "invalid skip request", http.StatusBadRequest)
			return
		}
	}

	mu.Lock()
	defer mu.Unlock()
	host, ok := currentHost(w, r)
	if !ok {
		return
	}

	skipped, err := host.room.SkipCurrentSong(strings.TrimSpace(body.SongID))
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "host"
	}
	log.Printf("song skipped (%s): %q\n%s", reason, skipped.Title, host.room)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(host.room.Queue)
}

func getGenres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(room.Genres)
}

func handleSongRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		submitSong(w, r, false)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	c, ok := currentCaller(w, r)
	if !ok {
		return
	}
	writeRequestState(w, c)
}

func writeRequestState(w http.ResponseWriter, c caller) {
	requests := make([]*room.SongRequest, 0)
	for _, request := range c.room.Requests {
		if c.room.IsHost(c.userID) || request.UserID == c.userID {
			requests = append(requests, request)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"roomName": c.room.Name,
		"genre":    c.room.Genre,
		"queue":    c.room.Queue,
		"requests": requests,
	})
}

func submitSong(w http.ResponseWriter, r *http.Request, legacy bool) {
	var song room.Song
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&song); err != nil {
		http.Error(w, "invalid song", http.StatusBadRequest)
		return
	}
	if !videoIDPattern.MatchString(song.VideoID) || song.Title == "" || len(song.Title) > 1000 || len(song.Channel) > 500 {
		http.Error(w, "invalid song data", http.StatusBadRequest)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	c, ok := currentCaller(w, r)
	if !ok {
		return
	}
	song.ID = room.GenerateUUIDv4()
	request := &room.SongRequest{
		ID: song.ID, Song: song, UserID: c.userID, RequestedBy: c.session.Name,
		Status: room.Pending, Reason: "Checking the room genre.",
	}
	c.room.Requests = append(c.room.Requests, request)
	if c.room.Genre == "Any" {
		request.Status, request.Reason = room.Allow, "No genre filter."
		releaseRoomRequests(c.room)
	} else if !c.room.ProcessingRequests {
		c.room.ProcessingRequests = true
		go processSongRequests(c.room, classifier, verifySong)
	}
	c.room.Save()
	w.Header().Set("Content-Type", "application/json")
	if legacy {
		json.NewEncoder(w).Encode(c.room.Queue)
	} else {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(request)
	}
}

// One worker per room preserves admission order; network calls never hold mu.
func processSongRequests(rm *room.Room, engine songClassifier, lookup func(context.Context, string) (room.Song, error)) {
	defer func() {
		if r := recover(); r != nil {
			log.Println("panic in processSongRequests:", r)
			mu.Lock()
			rm.ProcessingRequests = false
			mu.Unlock()
		}
	}()
	for {
		mu.Lock()
		var request *room.SongRequest
		for _, candidate := range rm.Requests {
			if candidate.Status == room.Pending {
				request = candidate
				break
			}
		}
		if request == nil {
			rm.ProcessingRequests = false
			mu.Unlock()
			return
		}
		song, genre := request.Song, rm.Genre
		mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		verified, err := lookup(ctx, song.VideoID)
		decision := verdict{room.Review, "YouTube metadata could not be verified. Host review required."}
		if err == nil {
			verified.ID = song.ID
			decision, err = engine.Classify(ctx, verified, genre)
			if err != nil || !room.ValidDecision(decision.Decision) || (decision.Decision != room.Allow && decision.Reason == "") {
				decision = verdict{room.Review, "AI check unavailable or invalid. Host review required."}
			}
		}
		cancel()

		mu.Lock()
		if err == nil || verified.VideoID != "" {
			request.Song = verified
			request.Song.ID = song.ID
		}
		session := sessions[request.UserID]
		if rooms[rm.Code()] != rm || session == nil || session.RoomCode != rm.Code() {
			decision = verdict{room.Block, "The requester is no longer in this room."}
		}
		request.Status, request.Reason = decision.Decision, decision.Reason
		releaseRoomRequests(rm)
		rm.Save()
		mu.Unlock()
	}
}

func decideSongRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		RequestID string `json:"requestId"`
		Decision  string `json:"decision"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		http.Error(w, "invalid decision", http.StatusBadRequest)
		return
	}
	mu.Lock()
	defer mu.Unlock()
	c, ok := currentHost(w, r)
	if !ok {
		return
	}
	if request := c.room.Request(body.RequestID); request != nil && body.Decision == room.Allow {
		session := sessions[request.UserID]
		if session == nil || session.RoomCode != c.room.Code() {
			http.Error(w, "requester is no longer in this room", http.StatusConflict)
			return
		}
	}
	releaseRoomRequests(c.room)
	if err := c.room.DecideRequest(body.RequestID, body.Decision); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	c.room.Save()
	writeRequestState(w, c)
}

func releaseRoomRequests(rm *room.Room) {
	for _, request := range rm.Requests {
		if request.Queued || request.Status == room.Block {
			continue
		}
		session := sessions[request.UserID]
		if session == nil || session.RoomCode != rm.Code() {
			request.Status, request.Reason = room.Block, "The requester is no longer in this room."
		}
	}
	rm.ReleaseApproved()
}
