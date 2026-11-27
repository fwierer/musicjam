package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

type classifierFunc func(context.Context, room.Song, string) (verdict, error)

func (fn classifierFunc) Classify(ctx context.Context, song room.Song, genre string) (verdict, error) {
	return fn(ctx, song, genre)
}

type requestState struct {
	Genre    string             `json:"genre"`
	Queue    []room.Song        `json:"queue"`
	Requests []room.SongRequest `json:"requests"`
}

func TestRoomMembersAndPersistence(t *testing.T) {
	previousRooms, previousSessions, previousDataDir := rooms, sessions, room.DataDir
	t.Cleanup(func() { rooms, sessions, room.DataDir = previousRooms, previousSessions, previousDataDir })
	room.DataDir = t.TempDir()
	rooms = map[string]*room.Room{}
	sessions = map[string]*Session{}
	mux := http.NewServeMux()
	registerHandlers(mux)

	request := func(method, path, user, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
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
	readMembers := func(w *httptest.ResponseRecorder) []room.Member {
		t.Helper()
		var members []room.Member
		if err := json.Unmarshal(w.Body.Bytes(), &members); err != nil {
			t.Fatal(err)
		}
		return members
	}

	expectStatus(request(http.MethodPost, "/api/room/create", "host", ""), http.StatusBadRequest)
	expectStatus(request(http.MethodPost, "/api/room/create?name=%20Host%20", "host", ""), http.StatusOK)
	code := sessions["host"].RoomCode
	expectStatus(request(http.MethodPost, "/api/room/join?code="+code+"&name=Guest", "guest", ""), http.StatusOK)
	expectStatus(request(http.MethodPost, "/api/room/join?code="+code+"&name=Other", "other", ""), http.StatusOK)

	w := request(http.MethodGet, "/api/room/getRoomMembers?code="+code, "", "")
	expectStatus(w, http.StatusOK)
	members := readMembers(w)
	if len(members) != 3 || !members[0].IsHost || members[0].Name != "Host" {
		t.Fatalf("wrong member list: %+v", members)
	}
	hostID, guestID := members[0].ID, members[1].ID

	remove := func(user, memberID string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"memberId": memberID})
		return request(http.MethodDelete, "/api/room/removeRoomMember", user, string(body))
	}
	expectStatus(remove("", guestID), http.StatusUnauthorized)
	expectStatus(remove("other", guestID), http.StatusForbidden)
	expectStatus(remove("host", hostID), http.StatusBadRequest)
	expectStatus(remove("host", guestID), http.StatusOK)
	expectStatus(request(http.MethodGet, "/api/room/queue", "guest", ""), http.StatusUnauthorized)

	// Simulate restart and verify persistence
	rooms = room.LoadExistingRoomsFromDatabase(room.DataDir)
	sessions = loadSessionsFromRooms(rooms)
	expectStatus(request(http.MethodGet, "/api/session", "host", ""), http.StatusOK)
	expectStatus(request(http.MethodGet, "/api/session", "other", ""), http.StatusOK)
}

func TestQueueAndHostOnlySkip(t *testing.T) {
	previousRooms, previousSessions, previousDataDir := rooms, sessions, room.DataDir
	t.Cleanup(func() { rooms, sessions, room.DataDir = previousRooms, previousSessions, previousDataDir })
	room.DataDir = t.TempDir()
	rm := room.NewRoom("host", "Host", func(string) bool { return false })
	rooms = map[string]*room.Room{rm.Code(): rm}
	sessions = map[string]*Session{
		"host":  {Name: "Host", RoomCode: rm.Code(), IsHost: true},
		"guest": {Name: "Guest", RoomCode: rm.Code()},
	}
	mux := http.NewServeMux()
	registerHandlers(mux)

	doReq := func(method, path, user, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
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

	// Empty queue skip -> 409 Conflict; wrong method -> 405; unauth -> 401; guest -> 403
	expectStatus(doReq(http.MethodGet, "/api/room/skip", "host", ""), http.StatusMethodNotAllowed)
	expectStatus(doReq(http.MethodPost, "/api/room/skip", "", `{}`), http.StatusUnauthorized)
	expectStatus(doReq(http.MethodPost, "/api/room/skip", "guest", `{}`), http.StatusForbidden)
	expectStatus(doReq(http.MethodPost, "/api/room/skip", "host", `{}`), http.StatusConflict)

	for _, user := range []string{"host", "guest", "host"} {
		w := doReq(http.MethodPost, "/api/room/queue", user, `{"videoId":"abcdefghijk","title":"Song","channel":"Artist"}`)
		expectStatus(w, http.StatusOK)
	}
	first, second, third := rm.Queue[0].ID, rm.Queue[1].ID, rm.Queue[2].ID

	// Guest cannot skip
	expectStatus(doReq(http.MethodPost, "/api/room/skip", "guest", `{"songId":"`+first+`"}`), http.StatusForbidden)
	if len(rm.Queue) != 3 {
		t.Fatal("guest skip altered queue")
	}

	// Host skips first song (e.g. copyright block)
	wSkip1 := doReq(http.MethodPost, "/api/room/skip", "host", `{"songId":"`+first+`","reason":"youtube_error_150"}`)
	expectStatus(wSkip1, http.StatusOK)
	q1 := readQueue(wSkip1)
	if len(q1) != 2 || q1[0].ID != second {
		t.Fatalf("unexpected queue after skip: %+v", q1)
	}

	// Duplicate skip for already-skipped first song returns 409 and does NOT skip second song
	expectStatus(doReq(http.MethodPost, "/api/room/skip", "host", `{"songId":"`+first+`"}`), http.StatusConflict)
	if len(rm.Queue) != 2 || rm.Queue[0].ID != second {
		t.Fatal("duplicate skip removed next song")
	}

	// Host skips current song without specifying songId
	wSkip2 := doReq(http.MethodPost, "/api/room/skip", "host", "")
	expectStatus(wSkip2, http.StatusOK)
	q2 := readQueue(wSkip2)
	if len(q2) != 1 || q2[0].ID != third {
		t.Fatalf("unexpected queue after manual skip: %+v", q2)
	}

	// Normal playback end via DELETE /api/room/queue removes third song
	wEnd := doReq(http.MethodDelete, "/api/room/queue", "host", `{"songId":"`+third+`"}`)
	expectStatus(wEnd, http.StatusOK)
	if len(readQueue(wEnd)) != 0 {
		t.Fatal("queue should be empty")
	}
}

func TestJoinOriginUsesLanIPInsteadOfLocalhost(t *testing.T) {
	t.Setenv("RUNNING_IN_DOCKER", "1")
	t.Setenv("PUBLIC_ORIGIN", "http://localhost:8000") // Accidental loopback PUBLIC_ORIGIN must be ignored
	t.Setenv("HOST_IP", "")

	// 1. Accessed via LAN IP in browser -> joinOrigin uses that LAN IP
	reqLan := httptest.NewRequest(http.MethodGet, "http://192.168.1.42:8000/api/session", nil)
	reqLan.Host = "192.168.1.42:8000"
	if got := getJoinOrigin(reqLan); got != "http://192.168.1.42:8000" {
		t.Fatalf("expected LAN IP joinOrigin, got %q", got)
	}

	// 2. HOST_IP env var overrides localhost request in Docker
	t.Setenv("HOST_IP", "192.168.1.99")
	reqLocal := httptest.NewRequest(http.MethodGet, "http://localhost:8000/api/session", nil)
	reqLocal.Host = "localhost:8000"
	if got := getJoinOrigin(reqLocal); got != "http://192.168.1.99:8000" {
		t.Fatalf("expected HOST_IP joinOrigin, got %q", got)
	}

	// 3. Explicit non-loopback PUBLIC_ORIGIN takes highest priority
	t.Setenv("PUBLIC_ORIGIN", "http://10.0.0.25:8000/")
	if got := getJoinOrigin(reqLocal); got != "http://10.0.0.25:8000" {
		t.Fatalf("expected PUBLIC_ORIGIN joinOrigin, got %q", got)
	}
}

func setupModeration(t *testing.T, engine songClassifier) (*http.ServeMux, string) {
	t.Helper()
	oldRooms, oldSessions, oldClassifier, oldLookup, oldDataDir := rooms, sessions, classifier, verifySong, room.DataDir
	room.DataDir = t.TempDir()
	rm := room.NewRoom("host", "Host", func(string) bool { return false })
	rm.Genre = "Rock"
	rm.AddMember("guest", "Guest")
	rooms = map[string]*room.Room{rm.Code(): rm}
	sessions = map[string]*Session{"host": {Name: "Host", RoomCode: rm.Code(), IsHost: true}, "guest": {Name: "Guest", RoomCode: rm.Code()}}
	classifier = engine
	verifySong = func(ctx context.Context, id string) (room.Song, error) {
		return room.Song{VideoID: id, Title: "Verified " + id, Channel: "Verified artist"}, nil
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(3 * time.Second)
		for {
			mu.Lock()
			busy := rm.ProcessingRequests
			mu.Unlock()
			if !busy {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("worker did not finish")
			}
			time.Sleep(time.Millisecond)
		}
		rooms, sessions, classifier, verifySong, room.DataDir = oldRooms, oldSessions, oldClassifier, oldLookup, oldDataDir
	})
	mux := http.NewServeMux()
	registerHandlers(mux)
	return mux, rm.Code()
}

func moderationRequest(t *testing.T, mux *http.ServeMux, method, path, user, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		r.AddCookie(&http.Cookie{Name: "userId", Value: user})
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func moderationState(t *testing.T, mux *http.ServeMux, user string) requestState {
	t.Helper()
	w := moderationRequest(t, mux, http.MethodGet, "/api/room/requests", user, "", http.StatusOK)
	var state requestState
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func awaitModeration(t *testing.T, mux *http.ServeMux, count int) requestState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := moderationState(t, mux, "host")
		ready := len(state.Requests) == count
		for _, request := range state.Requests {
			if request.Status == room.Pending {
				ready = false
			}
		}
		if ready {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("moderation did not finish")
	return requestState{}
}

func TestModerationAndBoundedContextClients(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	mux, _ := setupModeration(t, classifierFunc(func(ctx context.Context, song room.Song, genre string) (verdict, error) {
		if song.VideoID == "aaaaaaaaaaa" {
			close(entered)
			<-release
			return verdict{room.Review, "Uncertain genre."}, nil
		}
		return verdict{room.Allow, ""}, nil
	}))

	moderationRequest(t, mux, http.MethodPost, "/api/room/requests", "guest", `{"videoId":"aaaaaaaaaaa","title":"Browser title","channel":"Fake"}`, http.StatusAccepted)
	<-entered
	unblock()
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests", "host", `{"videoId":"bbbbbbbbbbb","title":"Browser title","channel":"Fake"}`, http.StatusAccepted)

	state := awaitModeration(t, mux, 2)
	if len(state.Queue) != 0 {
		t.Fatal("review request did not hold queue order")
	}
	decision := `{"requestId":"` + state.Requests[0].ID + `","decision":"allow"}`
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests/decision", "host", decision, http.StatusOK)
	state = moderationState(t, mux, "host")
	if len(state.Queue) != 2 || state.Queue[0].VideoID != "aaaaaaaaaaa" || state.Queue[1].VideoID != "bbbbbbbbbbb" {
		t.Fatalf("unexpected queue order: %+v", state.Queue)
	}

	// Verify YouTube & Player gateway forwarding
	ytSvc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"videoId":"zzzzzzzzzzz","title":"Canonical song","channel":"Canonical artist","thumbnail":"https://img.example/1.jpg"}]`))
		case "/lookup":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"videoId":"zzzzzzzzzzz","title":"Canonical song","channel":"Canonical artist"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ytSvc.Close()
	t.Setenv("YOUTUBE_URL", ytSvc.URL)

	wSearch := moderationRequest(t, mux, http.MethodGet, "/api/youtube/search?q=rock", "", "", http.StatusOK)
	var results []searchResult
	if err := json.NewDecoder(wSearch.Body).Decode(&results); err != nil || len(results) != 1 || results[0].VideoID != "zzzzzzzzzzz" {
		t.Fatalf("unexpected search results: %+v (%v)", results, err)
	}
	if _, err := lookupYouTubeSong(context.Background(), "zzzzzzzzzzz"); err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}

	// Classifier failure falls back to host review
	muxFail, _ := setupModeration(t, classifierFunc(func(context.Context, room.Song, string) (verdict, error) {
		return verdict{}, errors.New("offline")
	}))
	moderationRequest(t, muxFail, http.MethodPost, "/api/room/requests", "guest", `{"videoId":"ccccccccccc","title":"Song","channel":"Artist"}`, http.StatusAccepted)
	if st := awaitModeration(t, muxFail, 1); st.Requests[0].Status != room.Review {
		t.Fatalf("expected review on classifier failure, got %s", st.Requests[0].Status)
	}
}
