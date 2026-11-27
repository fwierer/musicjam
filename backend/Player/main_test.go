package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFallbackCleansMetadataRanksLyricsAndFallsBackToAudio(t *testing.T) {
	var seenQueries []string
	yt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		seenQueries = append(seenQueries, q)
		if strings.HasSuffix(q, "lyrics") {
			if strings.Contains(q, "Instrumental") {
				// Simulate empty lyrics results so service falls back to "audio" query
				json.NewEncoder(w).Encode([]searchCandidate{})
				return
			}
			json.NewEncoder(w).Encode([]searchCandidate{
				{VideoID: "origBlocked", Title: "BAND4BAND (Official Music Video)", Channel: "CentralCeeVEVO"},
				{VideoID: "topicVer123", Title: "BAND4BAND", Channel: "Central Cee - Topic"},
				{VideoID: "lyricsVer99", Title: "Central Cee - BAND4BAND (Lyrics)", Channel: "7clouds"},
			})
			return
		}
		if strings.HasSuffix(q, "audio") {
			json.NewEncoder(w).Encode([]searchCandidate{
				{VideoID: "audioVer777", Title: "Instrumental Piece (Audio)", Channel: "Indie Label"},
			})
			return
		}
		t.Fatalf("unexpected query: %s", q)
	}))
	defer yt.Close()

	t.Setenv("YOUTUBE_URL", yt.URL)
	srv := newPlayerServer()

	// 1. Official VEVO video fails -> cleans tags and selects untried 7clouds Lyrics video over - Topic
	body := `{"song":{"id":"1","videoId":"origBlocked","title":"BAND4BAND (Official Music Video)","channel":"CentralCeeVEVO"},"triedVideoIds":["origBlocked"]}`
	w := httptest.NewRecorder()
	srv.handleFallback(w, httptest.NewRequest(http.MethodPost, "/fallback", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp FallbackResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Action != "retry" || resp.VideoID != "lyricsVer99" {
		t.Fatalf("expected retry with lyricsVer99, got %+v", resp)
	}
	if len(seenQueries) != 1 || seenQueries[0] != "BAND4BAND CentralCee lyrics" {
		t.Fatalf("unexpected cleaned query: %+v", seenQueries)
	}

	// 2. Instrumental track with no lyrics results falls back to "<title> <channel> audio"
	seenQueries = nil
	instBody := `{"song":{"id":"2","videoId":"instBlocked","title":"Instrumental Piece [Official Video]","channel":"Composer - Topic"},"triedVideoIds":["instBlocked"]}`
	w2 := httptest.NewRecorder()
	srv.handleFallback(w2, httptest.NewRequest(http.MethodPost, "/fallback", strings.NewReader(instBody)))
	if w2.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 FallbackResponse
	_ = json.NewDecoder(w2.Body).Decode(&resp2)
	if resp2.Action != "retry" || resp2.VideoID != "audioVer777" {
		t.Fatalf("expected audio fallback retry, got %+v", resp2)
	}
	if len(seenQueries) != 2 || !strings.HasSuffix(seenQueries[1], "audio") {
		t.Fatalf("expected lyrics then audio query, got %+v", seenQueries)
	}
}

func TestFallbackMaxAttemptsAndValidation(t *testing.T) {
	srv := newPlayerServer()

	// Max attempts reached -> immediate skip without calling YouTube
	maxBody := `{"song":{"id":"1","videoId":"v1111111111","title":"Song","channel":"Artist"},"triedVideoIds":["v1111111111","v2222222222","v3333333333","v4444444444"]}`
	w := httptest.NewRecorder()
	srv.handleFallback(w, httptest.NewRequest(http.MethodPost, "/fallback", strings.NewReader(maxBody)))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp FallbackResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Action != "skip" {
		t.Fatalf("expected skip after max attempts, got %+v", resp)
	}

	w405 := httptest.NewRecorder()
	srv.handleFallback(w405, httptest.NewRequest(http.MethodGet, "/fallback", nil))
	if w405.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w405.Code)
	}

	w400 := httptest.NewRecorder()
	srv.handleFallback(w400, httptest.NewRequest(http.MethodPost, "/fallback", strings.NewReader(`{"song":{}}`)))
	if w400.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w400.Code)
	}
}
