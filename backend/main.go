package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

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
	loadEnv()

	mux := http.NewServeMux()
	registerHandlers(mux)
	rooms = room.LoadExistingRoomsFromDatabase("../database")
	sessions = loadSessionsFromRooms(rooms)
	localIp := GetLocalIP()

	server := &http.Server{
		Addr:              ":8000",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	log.Println("MusicJam running on http://" + localIp.String() + ":8000")
	log.Fatal(server.ListenAndServe())

}

// loadSessionsFromRooms rebuilds active user sessions directly from the members
// stored inside each room's JSON file. Rooms are processed from oldest to newest
// so a user who joined a newer room gets their latest room session.
func loadSessionsFromRooms(loadedRooms map[string]*room.Room) map[string]*Session {
	loaded := make(map[string]*Session)
	roomList := make([]*room.Room, 0, len(loadedRooms))
	for _, rm := range loadedRooms {
		roomList = append(roomList, rm)
	}
	sort.Slice(roomList, func(i, j int) bool {
		if roomList[i].UpdatedAt != roomList[j].UpdatedAt {
			return roomList[i].UpdatedAt < roomList[j].UpdatedAt
		}
		return roomList[i].Code() < roomList[j].Code()
	})
	for _, rm := range roomList {
		for userID, m := range rm.Members() {
			loaded[userID] = &Session{
				Name:     m.Name,
				RoomCode: rm.Code(),
				IsHost:   rm.IsHost(userID),
			}
		}
		for _, req := range rm.Requests {
			if req.Status == room.Pending && !rm.ProcessingRequests {
				rm.ProcessingRequests = true
				go processSongRequests(rm, classifier, verifySong)
				break
			}
		}
	}
	return loaded
}

// GetLocalIP returns the local preferred IP address used to host the backend
// so other devices on the same network can connect to the service.
func GetLocalIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Println("Unable to resolve local IP address: " + err.Error())
		return net.IPv4(127, 0, 0, 1)
	}
	defer conn.Close()
	localAddress := conn.LocalAddr().(*net.UDPAddr)
	return localAddress.IP
}

func loadEnv() {
	data, _ := os.ReadFile("../.env")
	for _, line := range strings.Split(string(data), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k != "" && !strings.HasPrefix(k, "#") {
			os.Setenv(k, v)
		}
	}
}


