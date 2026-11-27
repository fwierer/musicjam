package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxTotalAttempts = 4

var (
	videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	bracketNoiseRe = regexp.MustCompile(`(?i)[\(\[]\s*(official\s*(music\s*)?(video|audio|visualizer|lyric\s*video|hd|4k)?|music\s*video|visualizer|hd|4k|explicit|clean|remastered[^)\]]*)\s*[\)\]]`)
	topicSuffixRe  = regexp.MustCompile(`(?i)\s*-\s*topic$`)
	vevoSuffixRe   = regexp.MustCompile(`(?i)vevo$`)
	officialSuffRe = regexp.MustCompile(`(?i)\s+official$`)
	whitespaceRe   = regexp.MustCompile(`\s+`)
)

type Song struct {
	ID      string `json:"id,omitempty"`
	VideoID string `json:"videoId"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
}

type FallbackRequest struct {
	Song          Song     `json:"song"`
	TriedVideoIDs []string `json:"triedVideoIds"`
}

type FallbackResponse struct {
	Action  string `json:"action"`
	VideoID string `json:"videoId,omitempty"`
	Title   string `json:"title,omitempty"`
	Channel string `json:"channel,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type searchCandidate struct {
	VideoID   string `json:"videoId"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
}

type playerServer struct {
	youtubeURL string
	client     *http.Client
}

func newPlayerServer() *playerServer {
	baseURL := strings.TrimRight(os.Getenv("YOUTUBE_URL"), "/")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8081"
	}
	return &playerServer{
		youtubeURL: baseURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func cleanSearchTerms(title, channel string) (string, string) {
	cleanedTitle := bracketNoiseRe.ReplaceAllString(title, " ")
	cleanedTitle = strings.TrimSpace(whitespaceRe.ReplaceAllString(cleanedTitle, " "))
	if cleanedTitle == "" {
		cleanedTitle = strings.TrimSpace(title)
	}

	cleanedChannel := strings.TrimSpace(channel)
	cleanedChannel = topicSuffixRe.ReplaceAllString(cleanedChannel, "")
	cleanedChannel = vevoSuffixRe.ReplaceAllString(cleanedChannel, "")
	cleanedChannel = officialSuffRe.ReplaceAllString(cleanedChannel, "")
	cleanedChannel = strings.TrimSpace(whitespaceRe.ReplaceAllString(cleanedChannel, " "))
	return cleanedTitle, cleanedChannel
}

func fallbackCandidateScore(c searchCandidate) int {
	score := 0
	lowerTitle := strings.ToLower(c.Title)
	lowerChannel := strings.ToLower(strings.TrimSpace(c.Channel))

	if strings.Contains(lowerTitle, "lyrics") || strings.Contains(lowerTitle, "lyric") {
		score -= 2
	} else if strings.Contains(lowerTitle, "audio") {
		score--
	}

	if strings.HasSuffix(lowerChannel, "- topic") ||
		strings.HasSuffix(lowerChannel, "vevo") ||
		strings.HasSuffix(lowerChannel, " official") {
		score += 3
	}
	if strings.Contains(lowerTitle, "official music video") ||
		strings.Contains(lowerTitle, "official video") ||
		strings.Contains(lowerTitle, "(music video)") {
		score += 3
	}
	return score
}

func (s *playerServer) queryCandidates(ctx context.Context, query string) ([]searchCandidate, error) {
	endpoint := s.youtubeURL + "/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube service status %d", resp.StatusCode)
	}
	var results []searchCandidate
	if err := json.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&results); err != nil {
		return nil, err
	}
	return results, nil
}

func (s *playerServer) handleFallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req FallbackRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Song.Title) == "" {
		http.Error(w, "song title is required", http.StatusBadRequest)
		return
	}

	tried := make(map[string]bool, len(req.TriedVideoIDs)+1)
	if req.Song.VideoID != "" {
		tried[req.Song.VideoID] = true
	}
	for _, id := range req.TriedVideoIDs {
		if id = strings.TrimSpace(id); id != "" {
			tried[id] = true
		}
	}

	if len(tried) >= maxTotalAttempts {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FallbackResponse{
			Action: "skip",
			Reason: "Maximum fallback attempts reached.",
		})
		return
	}

	cleanTitle, cleanChannel := cleanSearchTerms(req.Song.Title, req.Song.Channel)
	base := strings.TrimSpace(cleanTitle + " " + cleanChannel)
	queries := []string{
		base + " lyrics",
		base + " audio",
	}

	var lastErr error
	anySuccess := false
	for _, q := range queries {
		candidates, err := s.queryCandidates(r.Context(), q)
		if err != nil {
			lastErr = err
			continue
		}
		anySuccess = true

		untried := make([]searchCandidate, 0, len(candidates))
		for _, c := range candidates {
			if videoIDPattern.MatchString(c.VideoID) && !tried[c.VideoID] {
				untried = append(untried, c)
			}
		}
		if len(untried) == 0 {
			continue
		}

		sort.SliceStable(untried, func(i, j int) bool {
			return fallbackCandidateScore(untried[i]) < fallbackCandidateScore(untried[j])
		})

		best := untried[0]
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(FallbackResponse{
			Action:  "retry",
			VideoID: best.VideoID,
			Title:   best.Title,
			Channel: best.Channel,
		})
		return
	}

	if !anySuccess && lastErr != nil {
		http.Error(w, "youtube search service unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(FallbackResponse{
		Action: "skip",
		Reason: "No playable embed version found.",
	})
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8083"
	}
	srv := newPlayerServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/fallback", srv.handleFallback)
	mux.HandleFunc("/healthz", handleHealthz)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	log.Println("Player fallback API listening on :" + port)
	log.Fatal(server.ListenAndServe())
}
