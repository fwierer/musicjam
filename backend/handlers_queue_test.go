package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

func TestQueuePlayback(t *testing.T) {
	previousRooms, previousSessions := rooms, sessions
	t.Cleanup(func() { rooms, sessions = previousRooms, previousSessions })
	rm := room.NewRoom("host", "Host", func(string) bool { return false })
	rooms = map[string]*room.Room{rm.Code(): rm}
	sessions = map[string]*Session{
		"host":  {Name: "Host", RoomCode: rm.Code(), IsHost: true},
		"guest": {Name: "Guest", RoomCode: rm.Code()},
	}
	mux := http.NewServeMux()
	registerHandlers(mux)
	request := func(method, user, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/room/queue", strings.NewReader(body))
		if user != "" {
			r.AddCookie(&http.Cookie{Name: "userId", Value: user})
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	expectStatus := func(w *httptest.ResponseRecorder, want int) {
		t.Helper()
		if w.Code != want {
			t.Fatalf("status = %d, want %d: %s", w.Code, want, w.Body.String())
		}
	}
	readQueue := func(w *httptest.ResponseRecorder) []room.Song {
		t.Helper()
		var queue []room.Song
		if err := json.Unmarshal(w.Body.Bytes(), &queue); err != nil {
			t.Fatal(err)
		}
		return queue
	}

	expectStatus(request(http.MethodDelete, "", `{"songId":"missing"}`), http.StatusUnauthorized)
	expectStatus(request(http.MethodDelete, "host", `{"songId":"missing"}`), http.StatusConflict)
	expectStatus(request(http.MethodDelete, "host", `{}`), http.StatusBadRequest)

	// Repeated videos are separate queue entries; host and guest additions keep their order.
	for i, user := range []string{"host", "guest", "host"} {
		w := request(http.MethodPost, user, `{"id":"client-id","videoId":"abcdefghijk","title":"Song","channel":"Artist"}`)
		expectStatus(w, http.StatusOK)
		queue := readQueue(w)
		if len(queue) != i+1 || queue[i].ID == "" || queue[i].ID == "client-id" {
			t.Fatalf("invalid queue after add: %+v", queue)
		}
	}
	first, second, third := rm.Queue[0].ID, rm.Queue[1].ID, rm.Queue[2].ID
	if first == second || second == third || first == third {
		t.Fatal("queue entries must have unique IDs")
	}
	end := func(user, id string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"songId": id})
		return request(http.MethodDelete, user, string(body))
	}
	expectStatus(end("guest", first), http.StatusForbidden)
	expectStatus(end("host", second), http.StatusConflict)
	if len(rm.Queue) != 3 {
		t.Fatal("rejected requests changed the queue")
	}
	w := end("host", first)
	expectStatus(w, http.StatusOK)
	queue := readQueue(w)
	if len(queue) != 2 || queue[0].ID != second || queue[1].ID != third {
		t.Fatalf("wrong playback order: %+v", queue)
	}
	expectStatus(end("host", first), http.StatusConflict)
	if len(rm.Queue) != 2 {
		t.Fatal("repeated end event removed the next song")
	}
	expectStatus(end("host", second), http.StatusOK)
	w = end("host", third)
	expectStatus(w, http.StatusOK)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("last song must leave an empty queue: %s", w.Body.String())
	}
	expectStatus(end("host", third), http.StatusConflict)
	w = request(http.MethodGet, "guest", "")
	expectStatus(w, http.StatusOK)
	if len(readQueue(w)) != 0 {
		t.Fatal("guest still sees finished songs")
	}
}
