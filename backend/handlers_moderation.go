package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

var classifier songClassifier = newOllamaClassifier()
var verifySong = lookupYouTubeSong
var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

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
	json.NewEncoder(w).Encode(map[string]any{"genre": c.room.Genre, "queue": c.room.Queue, "requests": requests})
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
	song.ID = rand.Text()
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
			if err != nil || !room.ValidDecision(decision.Decision) || decision.Reason == "" {
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
