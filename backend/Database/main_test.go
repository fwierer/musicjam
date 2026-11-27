package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDatabaseCRUDAndValidation(t *testing.T) {
	dir := t.TempDir()
	srv := newDBServer(dir)
	mux := http.NewServeMux()
	registerRoutes(mux, srv)

	// Health check
	wHealth := httptest.NewRecorder()
	mux.ServeHTTP(wHealth, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if wHealth.Code != http.StatusOK {
		t.Fatalf("healthz status = %d", wHealth.Code)
	}

	// Empty rooms returns []
	wEmpty := httptest.NewRecorder()
	mux.ServeHTTP(wEmpty, httptest.NewRequest(http.MethodGet, "/rooms", nil))
	if wEmpty.Code != http.StatusOK || strings.TrimSpace(wEmpty.Body.String()) != "[]" {
		t.Fatalf("unexpected empty /rooms: %d %q", wEmpty.Code, wEmpty.Body.String())
	}

	// Invalid code / path traversal rejected
	for _, badPath := range []string{"/rooms/", "/rooms/bad.code", "/rooms/a/b"} {
		wBad := httptest.NewRecorder()
		mux.ServeHTTP(wBad, httptest.NewRequest(http.MethodGet, badPath, nil))
		if wBad.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for %s, got %d", badPath, wBad.Code)
		}
	}
	wTraversal := httptest.NewRecorder()
	srv.handleRoomByCode(wTraversal, httptest.NewRequest(http.MethodGet, "/rooms/../etc", nil))
	if wTraversal.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for path traversal in handler, got %d", wTraversal.Code)
	}

	// Missing room returns 404
	wMissing := httptest.NewRecorder()
	mux.ServeHTTP(wMissing, httptest.NewRequest(http.MethodGet, "/rooms/11111111-1111-4111-8111-111111111111", nil))
	if wMissing.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", wMissing.Code)
	}

	// Invalid JSON rejected
	code := "11111111-1111-4111-8111-111111111111"
	wInvalidJSON := httptest.NewRecorder()
	mux.ServeHTTP(wInvalidJSON, httptest.NewRequest(http.MethodPut, "/rooms/"+code, strings.NewReader("{bad")))
	if wInvalidJSON.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid json, got %d", wInvalidJSON.Code)
	}

	// PUT valid room JSON -> saved atomically on disk
	roomPayload := `{"room_code":"` + code + `","name":"Friday Jam","host_id":"h1","members":{},"queue":[],"genre":"Rock","requests":[]}`
	wPut := httptest.NewRecorder()
	mux.ServeHTTP(wPut, httptest.NewRequest(http.MethodPut, "/rooms/"+code, strings.NewReader(roomPayload)))
	if wPut.Code != http.StatusOK {
		t.Fatalf("expected 200 on PUT, got %d: %s", wPut.Code, wPut.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "room_"+code+".json")); err != nil {
		t.Fatalf("expected room JSON file on disk: %v", err)
	}

	// GET single room
	wGet := httptest.NewRecorder()
	mux.ServeHTTP(wGet, httptest.NewRequest(http.MethodGet, "/rooms/"+code, nil))
	if wGet.Code != http.StatusOK || wGet.Body.String() != roomPayload {
		t.Fatalf("unexpected GET room: %d %q", wGet.Code, wGet.Body.String())
	}

	// GET all rooms
	wAll := httptest.NewRecorder()
	mux.ServeHTTP(wAll, httptest.NewRequest(http.MethodGet, "/rooms", nil))
	if wAll.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /rooms, got %d", wAll.Code)
	}
	var listed []json.RawMessage
	if err := json.Unmarshal(wAll.Body.Bytes(), &listed); err != nil || len(listed) != 1 {
		t.Fatalf("expected 1 room in list, got %+v (err=%v)", listed, err)
	}

	// Unsupported method -> 405
	w405 := httptest.NewRecorder()
	mux.ServeHTTP(w405, httptest.NewRequest(http.MethodPost, "/rooms/"+code, nil))
	if w405.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w405.Code)
	}

	// DELETE room removes file
	wDel := httptest.NewRecorder()
	mux.ServeHTTP(wDel, httptest.NewRequest(http.MethodDelete, "/rooms/"+code, nil))
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d", wDel.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "room_"+code+".json")); !os.IsNotExist(err) {
		t.Fatalf("expected room JSON file deleted, err=%v", err)
	}
}
