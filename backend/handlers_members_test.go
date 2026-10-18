package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

func TestRoomMembers(t *testing.T) {
	previousRooms, previousSessions := rooms, sessions
	t.Cleanup(func() { rooms, sessions = previousRooms, previousSessions })
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

	// A name is required to create or join a room
	expectStatus(request(http.MethodPost, "/api/room/create", "host", ""), http.StatusBadRequest)
	expectStatus(request(http.MethodPost, "/api/room/create?name=%20%20", "host", ""), http.StatusBadRequest)
	expectStatus(request(http.MethodPost, "/api/room/create?name="+strings.Repeat("a", room.MaxNameLength+1), "host", ""), http.StatusBadRequest)
	if len(rooms) != 0 {
		t.Fatal("room created without a valid name")
	}
	expectStatus(request(http.MethodPost, "/api/room/create?name=%20Host%20", "host", ""), http.StatusOK)
	code := sessions["host"].RoomCode
	if sessions["host"].Name != "Host" {
		t.Fatalf("name not trimmed: %q", sessions["host"].Name)
	}
	expectStatus(request(http.MethodPost, "/api/room/join?code="+code, "guest", ""), http.StatusBadRequest)
	if sessions["guest"] != nil {
		t.Fatal("joined without a name")
	}
	expectStatus(request(http.MethodPost, "/api/room/join?code="+code+"&name=Guest", "guest", ""), http.StatusOK)
	expectStatus(request(http.MethodPost, "/api/room/join?code="+code+"&name=Other", "other", ""), http.StatusOK)

	// The member list must not leak the user IDs, they are the session cookies
	w := request(http.MethodGet, "/api/room/getRoomMembers?code="+code, "", "")
	expectStatus(w, http.StatusOK)
	if body := w.Body.String(); strings.Contains(body, `"host"`) || strings.Contains(body, `"guest"`) || strings.Contains(body, `"other"`) {
		t.Fatalf("member list leaks user IDs: %s", body)
	}
	members := readMembers(w)
	if len(members) != 3 || !members[0].IsHost || members[0].Name != "Host" {
		t.Fatalf("wrong member list: %+v", members)
	}
	hostID, guestID := members[0].ID, members[1].ID

	remove := func(user, memberID string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"memberId": memberID})
		return request(http.MethodDelete, "/api/room/removeRoomMember", user, string(body))
	}
	expectStatus(request(http.MethodPost, "/api/room/removeRoomMember", "host", `{"memberId":"`+guestID+`"}`), http.StatusMethodNotAllowed)
	expectStatus(remove("", guestID), http.StatusUnauthorized)
	expectStatus(remove("other", guestID), http.StatusForbidden)
	expectStatus(remove("host", hostID), http.StatusBadRequest)
	expectStatus(remove("host", "missing"), http.StatusNotFound)
	expectStatus(request(http.MethodDelete, "/api/room/removeRoomMember", "host", `{}`), http.StatusBadRequest)

	w = remove("host", guestID)
	expectStatus(w, http.StatusOK)
	if members := readMembers(w); len(members) != 2 || members[1].Name != "Other" {
		t.Fatalf("wrong members after removal: %+v", members)
	}
	// The removed guest is locked out of the room, the others are not
	expectStatus(request(http.MethodGet, "/api/room/queue", "guest", ""), http.StatusUnauthorized)
	expectStatus(request(http.MethodGet, "/api/room/queue", "other", ""), http.StatusOK)
	expectStatus(remove("host", guestID), http.StatusNotFound)

	// Removing a stale member must not end the session they have in another room
	expectStatus(request(http.MethodPost, "/api/room/create?name=Other%20host", "otherHost", ""), http.StatusOK)
	otherCode := sessions["otherHost"].RoomCode
	expectStatus(request(http.MethodPost, "/api/room/join?code="+otherCode+"&name=Other", "other", ""), http.StatusOK)
	otherID := readMembers(request(http.MethodGet, "/api/room/getRoomMembers?code="+code, "", ""))[1].ID
	expectStatus(remove("host", otherID), http.StatusOK)
	if s := sessions["other"]; s == nil || s.RoomCode != otherCode {
		t.Fatal("removal ended the member's session in another room")
	}
}
