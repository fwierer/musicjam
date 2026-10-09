package main

import "net/http"

func registerHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/youtube/search", handleYouTubeSearch)
	mux.Handle("/", http.FileServer(http.Dir("../frontend")))
}
