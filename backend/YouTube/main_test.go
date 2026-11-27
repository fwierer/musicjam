package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSearchFiltersCopyrightBlocksRanksPlayableAndCachesLookup(t *testing.T) {
	var searchCalls, videoCalls atomic.Int32

	ytAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/youtube/v3/search":
			searchCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":      map[string]string{"videoId": "ageRestrVid"},
						"snippet": map[string]any{"title": "Deutschland (Official Video)", "channelTitle": "Rammstein Official"},
					},
					{
						"id":      map[string]string{"videoId": "geoBlockVid"},
						"snippet": map[string]any{"title": "Geo Blocked Song", "channelTitle": "Indie Channel"},
					},
					{
						"id":      map[string]string{"videoId": "topicArtVid"},
						"snippet": map[string]any{"title": "rockstar", "channelTitle": "Post Malone - Topic"},
					},
					{
						"id":      map[string]string{"videoId": "vevoOffiVid"},
						"snippet": map[string]any{"title": "BAND4BAND (Official Music Video)", "channelTitle": "CentralCeeVEVO"},
					},
					{
						"id": map[string]string{"videoId": "playableLyr"},
						"snippet": map[string]any{
							"title":        "BAND4BAND (Lyrics)",
							"channelTitle": "7clouds",
							"thumbnails":   map[string]any{"default": map[string]string{"url": "https://img.example/lyr.jpg"}},
						},
					},
				},
			})
		case "/youtube/v3/videos":
			videoCalls.Add(1)
			if r.URL.Query().Get("part") != "snippet,status,contentDetails" {
				t.Errorf("unexpected part: %s", r.URL.Query().Get("part"))
			}
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":             "ageRestrVid",
						"snippet":        map[string]any{"title": "Deutschland (Official Video)", "channelTitle": "Rammstein Official"},
						"status":         map[string]any{"embeddable": true, "privacyStatus": "public"},
						"contentDetails": map[string]any{"contentRating": map[string]string{"ytRating": "ytAgeRestricted"}},
					},
					{
						"id":      "geoBlockVid",
						"snippet": map[string]any{"title": "Geo Blocked Song", "channelTitle": "Indie Channel"},
						"status":  map[string]any{"embeddable": true, "privacyStatus": "public"},
						"contentDetails": map[string]any{
							"regionRestriction": map[string]any{"blocked": []string{"IT", "DE"}},
						},
					},
					{
						"id":      "topicArtVid",
						"snippet": map[string]any{"title": "rockstar", "channelTitle": "Post Malone - Topic"},
						"status":  map[string]any{"embeddable": true, "privacyStatus": "public"},
					},
					{
						"id":      "vevoOffiVid",
						"snippet": map[string]any{"title": "BAND4BAND (Official Music Video)", "channelTitle": "CentralCeeVEVO"},
						"status":  map[string]any{"embeddable": true, "privacyStatus": "public"},
					},
					{
						"id": "playableLyr",
						"snippet": map[string]any{
							"title":        "BAND4BAND (Lyrics)",
							"channelTitle": "7clouds",
							"thumbnails":   map[string]any{"default": map[string]string{"url": "https://img.example/lyr.jpg"}},
						},
						"status": map[string]any{"embeddable": true, "privacyStatus": "public"},
					},
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ytAPI.Close()

	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_REGION", "IT")
	t.Setenv("YOUTUBE_API_BASE_URL", ytAPI.URL)

	srv := newYouTubeServer()

	w := httptest.NewRecorder()
	srv.handleSearch(w, httptest.NewRequest(http.MethodGet, "/search?q=band4band", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("search status %d: %s", w.Code, w.Body.String())
	}

	var results []SearchResult
	if err := json.NewDecoder(w.Body).Decode(&results); err != nil {
		t.Fatal(err)
	}
	// ageRestrVid and geoBlockVid must be dropped; playableLyr must rank ahead of Topic and VEVO.
	if len(results) != 3 {
		t.Fatalf("expected 3 playable results, got %d: %+v", len(results), results)
	}
	if results[0].VideoID != "playableLyr" {
		t.Fatalf("expected embed-friendly lyrics video ranked first, got %+v", results)
	}
	for _, r := range results {
		if r.VideoID == "ageRestrVid" || r.VideoID == "geoBlockVid" {
			t.Fatalf("unplayable video leaked into search results: %+v", r)
		}
	}

	// Subsequent lookup for a verified search result must hit the cache (zero extra API calls).
	wLookup := httptest.NewRecorder()
	srv.handleLookup(wLookup, httptest.NewRequest(http.MethodGet, "/lookup?id=playableLyr", nil))
	if wLookup.Code != http.StatusOK {
		t.Fatalf("lookup status %d: %s", wLookup.Code, wLookup.Body.String())
	}
	var song Song
	if err := json.NewDecoder(wLookup.Body).Decode(&song); err != nil {
		t.Fatal(err)
	}
	if song.Title != "BAND4BAND (Lyrics)" || song.Channel != "7clouds" {
		t.Fatalf("unexpected lookup song: %+v", song)
	}
	if searchCalls.Load() != 1 || videoCalls.Load() != 1 {
		t.Fatalf("expected 1 search and 1 videos call, got %d and %d", searchCalls.Load(), videoCalls.Load())
	}
}

func TestLookupCacheMissAndRestrictedRejection(t *testing.T) {
	var calls atomic.Int32
	ytAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		id := r.URL.Query().Get("id")
		if id == "blocked1234" {
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"id":             "blocked1234",
						"snippet":        map[string]any{"title": "Age Restricted", "channelTitle": "Artist"},
						"status":         map[string]any{"embeddable": true, "privacyStatus": "public"},
						"contentDetails": map[string]any{"contentRating": map[string]string{"ytRating": "ytAgeRestricted"}},
					},
				},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":      "zzzzzzzzzzz",
					"snippet": map[string]any{"title": "Canonical song", "channelTitle": "Canonical artist"},
					"status":  map[string]any{"embeddable": true, "privacyStatus": "public"},
				},
			},
		})
	}))
	defer ytAPI.Close()

	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_API_BASE_URL", ytAPI.URL)
	srv := newYouTubeServer()

	for range 2 {
		w := httptest.NewRecorder()
		srv.handleLookup(w, httptest.NewRequest(http.MethodGet, "/lookup?id=zzzzzzzzzzz", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("lookup status %d: %s", w.Code, w.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected cached lookup on second call, got %d calls", calls.Load())
	}

	wBlocked := httptest.NewRecorder()
	srv.handleLookup(wBlocked, httptest.NewRequest(http.MethodGet, "/lookup?id=blocked1234", nil))
	if wBlocked.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for age-restricted lookup, got %d", wBlocked.Code)
	}
}

func TestValidationAndMethodChecks(t *testing.T) {
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	srv := newYouTubeServer()

	w := httptest.NewRecorder()
	srv.handleSearch(w, httptest.NewRequest(http.MethodPost, "/search?q=rock", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.handleSearch(w, httptest.NewRequest(http.MethodGet, "/search", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.handleLookup(w, httptest.NewRequest(http.MethodGet, "/lookup?id=short", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
