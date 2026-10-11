package main

import (
	"log"
	"net/http"
)

func main() {
	mux := http.NewServeMux()
	registerHandlers(mux)

	log.Println("MusicJam listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
