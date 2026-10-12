package main

import (
	"log"
	"net/http"
	"sync"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

// mu guards rooms, sessions and the rooms' contents, since every request runs on its own goroutine
var mu sync.Mutex
var rooms = map[string]*room.Room{}
var sessions = map[string]*Session{} // key: user ID from the cookie

type Session struct {
	Name     string `json:"name"`
	RoomCode string `json:"roomCode"`
	IsHost   bool   `json:"isHost"`
}

func main() {

	mux := http.NewServeMux()
	registerHandlers(mux)

	log.Println("MusicJam listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
