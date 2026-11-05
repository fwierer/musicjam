package main

import (
	"log"
	"net"
	"net/http"
	"os"
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

// get local preferred IP address and use it to host backend
// because of this other devices int the same newtork will be able to connect to the sevice
func GetLocalIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Println("Unbale to resolve loal IP address" + err.Error())
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


