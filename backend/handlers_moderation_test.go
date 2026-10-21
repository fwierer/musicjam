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

func setupModeration(t *testing.T, engine songClassifier) (*http.ServeMux, string) {
	t.Helper()
	oldRooms, oldSessions, oldClassifier, oldLookup := rooms, sessions, classifier, verifySong
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
		rooms, sessions, classifier, verifySong = oldRooms, oldSessions, oldClassifier, oldLookup
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

func submitForTest(t *testing.T, mux *http.ServeMux, user, id string) {
	t.Helper()
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests", user, `{"videoId":"`+id+`","title":"Forged browser title","channel":"Fake"}`, http.StatusAccepted)
}

func TestModerationAdmissionAndHostDecisions(t *testing.T) {
	mux, _ := setupModeration(t, classifierFunc(func(ctx context.Context, song room.Song, genre string) (verdict, error) {
		if song.Title == "Forged browser title" || song.Channel != "Verified artist" || genre != "Rock" {
			t.Error("unverified input sent to classifier")
		}
		switch song.VideoID {
		case "aaaaaaaaaaa":
			return verdict{room.Review, "Uncertain genre."}, nil
		case "bbbbbbbbbbb":
			return verdict{room.Allow, "Fits rock."}, nil
		default:
			return verdict{room.Block, "Classical recording."}, nil
		}
	}))
	submitForTest(t, mux, "guest", "aaaaaaaaaaa")
	submitForTest(t, mux, "host", "bbbbbbbbbbb")
	submitForTest(t, mux, "guest", "ccccccccccc")
	state := awaitModeration(t, mux, 3)
	if len(state.Queue) != 0 {
		t.Fatal("later approval bypassed review")
	}
	if state.Requests[0].Song.Title != "Verified aaaaaaaaaaa" {
		t.Fatal("browser metadata was not replaced")
	}
	if len(moderationState(t, mux, "guest").Requests) != 2 {
		t.Fatal("guest sees other users' requests")
	}
	decision := `{"requestId":"` + state.Requests[0].ID + `","decision":"allow"}`
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests/decision", "guest", decision, http.StatusForbidden)
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests/decision", "host", decision, http.StatusOK)
	state = moderationState(t, mux, "host")
	if len(state.Queue) != 2 || state.Queue[0].VideoID != "aaaaaaaaaaa" || state.Queue[1].VideoID != "bbbbbbbbbbb" {
		t.Fatal("wrong admission order", state.Queue)
	}
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests/decision", "host", decision, http.StatusConflict)
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests/decision", "host", `{"requestId":"`+state.Requests[2].ID+`","decision":"allow"}`, http.StatusOK)
	state = moderationState(t, mux, "host")
	if len(state.Queue) != 3 || state.Queue[2].VideoID != "ccccccccccc" {
		t.Fatal("host override not appended")
	}
	moderationRequest(t, mux, http.MethodGet, "/api/room/requests", "", "", http.StatusUnauthorized)
	moderationRequest(t, mux, http.MethodPost, "/api/room/requests", "guest", `{"videoId":"bad","title":"Invalid"}`, http.StatusBadRequest)
	moderationRequest(t, mux, http.MethodPost, "/api/room/create?name=Host&genre=Invalid", "host", "", http.StatusBadRequest)
}

func TestModerationFailuresRequireReview(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "invalid output"}[invalid], func(t *testing.T) {
			mux, _ := setupModeration(t, classifierFunc(func(context.Context, room.Song, string) (verdict, error) {
				if invalid {
					return verdict{"execute", "bad"}, nil
				}
				return verdict{}, errors.New("offline")
			}))
			submitForTest(t, mux, "guest", "aaaaaaaaaaa")
			state := awaitModeration(t, mux, 1)
			if state.Requests[0].Status != room.Review || len(state.Queue) != 0 {
				t.Fatal("failure admitted a song")
			}
		})
	}
	mux, _ := setupModeration(t, classifierFunc(func(context.Context, room.Song, string) (verdict, error) {
		t.Error("classifier must not receive unverified metadata")
		return verdict{room.Allow, "ok"}, nil
	}))
	verifySong = func(context.Context, string) (room.Song, error) {
		return room.Song{}, errors.New("metadata unavailable")
	}
	submitForTest(t, mux, "guest", "aaaaaaaaaaa")
	if state := awaitModeration(t, mux, 1); state.Requests[0].Status != room.Review || len(state.Queue) != 0 {
		t.Fatal("metadata failure admitted a song")
	}
}

func TestModerationDoesNotLockRoomsAndRechecksMembership(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	mux, _ := setupModeration(t, classifierFunc(func(context.Context, room.Song, string) (verdict, error) {
		close(entered)
		<-release
		return verdict{room.Allow, "Fits rock."}, nil
	}))
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	submitForTest(t, mux, "guest", "aaaaaaaaaaa")
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("classifier not called")
	}
	finished := make(chan struct{})
	go func() { moderationState(t, mux, "host"); close(finished) }()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("AI request holds global mutex")
	}
	mu.Lock()
	delete(sessions, "guest")
	mu.Unlock()
	unblock()
	state := awaitModeration(t, mux, 1)
	if state.Requests[0].Status != room.Block || len(state.Queue) != 0 {
		t.Fatal("removed guest's song was admitted")
	}
}
