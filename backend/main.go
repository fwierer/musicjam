package main

import (
	"log"
	"net/http"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

func main() {
	nRoom := room.NewRoom()
	println(nRoom.Code())

	mux := http.NewServeMux()
	registerHandlers(mux)

	log.Println("MusicJam listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
