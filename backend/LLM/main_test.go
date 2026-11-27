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
	var calls atomic.Int32
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
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
			Theme string `json:"theme"`
		}
		_ = json.Unmarshal([]byte(body.Messages[1].Content), &meta)
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

	t.Setenv("OLLAMA_URL", ollama.URL)
	srv := newLLMServer()

	reqBody := `{"song":{"id":"1","videoId":"abcdefghijk","title":"Back in Black","channel":"AC/DC"},"theme":"Rock"}`
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
		if resp.Decision != "yes" || resp.Reason != "" || resp.Song.Title != "Back in Black" {
			t.Fatalf("unexpected yes response: %+v", resp)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected cached response on second call, got %d calls", calls.Load())
	}

	rejectBody := `{"song":{"id":"1","videoId":"abcdefghijk","title":"Back in Black","channel":"AC/DC"},"theme":"Classical"}`
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
	if resp.Decision != "no" || resp.Reason != "Does not match Classical theme." || resp.Song.VideoID != "abcdefghijk" {
		t.Fatalf("unexpected no response: %+v", resp)
	}
}

func TestModerateValidationAndInvalidOllamaOutput(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"done":    true,
			"message": map[string]string{"content": `{"decision":"no","reason":""}`},
		})
	}))
	defer ollama.Close()
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
		t.Fatalf("expected 400, got %d", w.Code)
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
