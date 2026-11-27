package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestModerateEndpointYesNoAndCache(t *testing.T) {
	var ollamaCalls, youtubeCalls atomic.Int32
	youtube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		youtubeCalls.Add(1)
		if r.URL.Path != "/lookup" || r.URL.Query().Get("id") != "abcdefghijk" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"videoId":"abcdefghijk","title":"Back in Black","channel":"AC/DC"}`))
	}))
	defer youtube.Close()

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ollamaCalls.Add(1)
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		var meta struct {
			Theme   string `json:"theme"`
			Title   string `json:"title"`
			Channel string `json:"channel"`
		}
		_ = json.Unmarshal([]byte(body.Messages[1].Content), &meta)
		if meta.Title != "Back in Black" || meta.Channel != "AC/DC" {
			t.Fatalf("expected verified YouTube metadata in Ollama prompt, got %+v", meta)
		}
		if meta.Theme == "Rock" {
			json.NewEncoder(w).Encode(map[string]any{
				"done":    true,
				"message": map[string]string{"content": `{"decision":"yes","reason":"ignored"}`},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"done":    true,
			"message": map[string]string{"content": `{"decision":"no","reason":"Does not match Classical theme."}`},
		})
	}))
	defer ollama.Close()

	t.Setenv("YOUTUBE_URL", youtube.URL)
	t.Setenv("OLLAMA_URL", ollama.URL)
	srv := newLLMServer()

	// Send spoofed client title/channel; LLM must fetch and use verified YouTube metadata instead.
	reqBody := `{"song":{"id":"1","videoId":"abcdefghijk","title":"Spoofed Title","channel":"Fake Channel"},"theme":"Rock"}`
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/moderate", strings.NewReader(reqBody))
		w := httptest.NewRecorder()
		srv.handleModerate(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status %d: %s", w.Code, w.Body.String())
		}
		var resp ModerateResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		if resp.Decision != "yes" || resp.Reason != "" || resp.Song.ID != "1" || resp.Song.Title != "Back in Black" || resp.Song.Channel != "AC/DC" {
			t.Fatalf("unexpected yes response: %+v", resp)
		}
	}
	if ollamaCalls.Load() != 1 {
		t.Fatalf("expected cached response on second call, got %d Ollama calls", ollamaCalls.Load())
	}
	if youtubeCalls.Load() != 2 {
		t.Fatalf("expected 2 YouTube lookup calls, got %d", youtubeCalls.Load())
	}

	rejectBody := `{"song":{"id":"1","videoId":"abcdefghijk","title":"Spoofed Title","channel":"Fake Channel"},"theme":"Classical"}`
	req := httptest.NewRequest(http.MethodPost, "/moderate", strings.NewReader(rejectBody))
	w := httptest.NewRecorder()
	srv.handleModerate(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	var resp ModerateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Decision != "no" || resp.Reason != "Does not match Classical theme." || resp.Song.VideoID != "abcdefghijk" || resp.Song.Title != "Back in Black" {
		t.Fatalf("unexpected no response: %+v", resp)
	}
}

func TestModerateValidationAndInvalidOllamaOutput(t *testing.T) {
	youtube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"videoId":"abcdefghijk","title":"Song","channel":"Artist"}`))
	}))
	defer youtube.Close()

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"done":    true,
			"message": map[string]string{"content": `{"decision":"no","reason":""}`},
		})
	}))
	defer ollama.Close()
	t.Setenv("YOUTUBE_URL", youtube.URL)
	t.Setenv("OLLAMA_URL", ollama.URL)
	srv := newLLMServer()

	w := httptest.NewRecorder()
	srv.handleModerate(w, httptest.NewRequest(http.MethodGet, "/moderate", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.handleModerate(w, httptest.NewRequest(http.MethodPost, "/moderate", strings.NewReader(`{"song":{},"theme":""}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing videoId/theme, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	srv.handleModerate(w, httptest.NewRequest(http.MethodPost, "/moderate", strings.NewReader(`{"song":{"videoId":"short"},"theme":"Rock"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid videoId, got %d", w.Code)
	}

	body, _ := json.Marshal(ModerateRequest{
		Song:  Song{VideoID: "abcdefghijk", Title: "Song", Channel: "Artist"},
		Theme: "Rock",
	})
	w = httptest.NewRecorder()
	srv.handleModerate(w, httptest.NewRequest(http.MethodPost, "/moderate", strings.NewReader(string(body))))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for missing rejection reason, got %d", w.Code)
	}
}

func TestModerateFailsWhenYouTubeUnavailable(t *testing.T) {
	var ollamaCalls atomic.Int32
	youtube := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failure", http.StatusBadGateway)
	}))
	defer youtube.Close()

	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ollamaCalls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"done":    true,
			"message": map[string]string{"content": `{"decision":"yes","reason":""}`},
		})
	}))
	defer ollama.Close()

	t.Setenv("YOUTUBE_URL", youtube.URL)
	t.Setenv("OLLAMA_URL", ollama.URL)
	srv := newLLMServer()

	body := `{"song":{"id":"1","videoId":"abcdefghijk"},"theme":"Rock"}`
	w := httptest.NewRecorder()
	srv.handleModerate(w, httptest.NewRequest(http.MethodPost, "/moderate", strings.NewReader(body)))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 when youtube lookup fails, got %d: %s", w.Code, w.Body.String())
	}
	if ollamaCalls.Load() != 0 {
		t.Fatalf("expected 0 Ollama calls when YouTube lookup fails, got %d", ollamaCalls.Load())
	}
}
