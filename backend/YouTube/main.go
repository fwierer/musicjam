package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Song struct {
	ID      string `json:"id,omitempty"`
	VideoID string `json:"videoId"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
}

type SearchResult struct {
	VideoID   string `json:"videoId"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
}

type youtubeSearchResponse struct {
	Items []struct {
		ID struct {
			VideoID string `json:"videoId"`
		} `json:"id"`
		Snippet struct {
			Title        string `json:"title"`
			ChannelTitle string `json:"channelTitle"`
			Thumbnails   struct {
				Default struct {
					URL string `json:"url"`
				} `json:"default"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
}

type youtubeVideosResponse struct {
	Items []youtubeVideoDetails `json:"items"`
}

type youtubeVideoDetails struct {
	ID      string `json:"id"`
	Snippet struct {
		Title        string `json:"title"`
		ChannelTitle string `json:"channelTitle"`
		Thumbnails   struct {
			Default struct {
				URL string `json:"url"`
			} `json:"default"`
		} `json:"thumbnails"`
	} `json:"snippet"`
	Status struct {
		UploadStatus  string `json:"uploadStatus"`
		PrivacyStatus string `json:"privacyStatus"`
		Embeddable    bool   `json:"embeddable"`
	} `json:"status"`
	ContentDetails struct {
		RegionRestriction *struct {
			Allowed []string `json:"allowed"`
			Blocked []string `json:"blocked"`
		} `json:"regionRestriction"`
		ContentRating struct {
			YtRating string `json:"ytRating"`
		} `json:"contentRating"`
	} `json:"contentDetails"`
}

type cachedSong struct {
	song    Song
	expires time.Time
}

type youtubeServer struct {
	apiKey     string
	regionCode string
	baseURL    string
	client     *http.Client
	mu         sync.Mutex
	cache      map[string]cachedSong
}

func newYouTubeServer() *youtubeServer {
	baseURL := strings.TrimRight(os.Getenv("YOUTUBE_API_BASE_URL"), "/")
	if baseURL == "" {
		baseURL = "https://www.googleapis.com"
	}
	regionCode := strings.TrimSpace(os.Getenv("YOUTUBE_REGION"))
	if regionCode == "" {
		regionCode = "IT"
	}
	return &youtubeServer{
		apiKey:     strings.TrimSpace(os.Getenv("YOUTUBE_API_KEY")),
		regionCode: regionCode,
		baseURL:    baseURL,
		client:     &http.Client{Timeout: 10 * time.Second},
		cache:      map[string]cachedSong{},
	}
}

func isPlayableInRegion(v youtubeVideoDetails, regionCode string) bool {
	if v.ID == "" || !v.Status.Embeddable || strings.TrimSpace(v.Snippet.Title) == "" {
		return false
	}
	switch strings.ToLower(v.Status.UploadStatus) {
	case "rejected", "failed", "deleted":
		return false
	}
	if v.Status.PrivacyStatus != "" && v.Status.PrivacyStatus != "public" && v.Status.PrivacyStatus != "unlisted" {
		return false
	}
	// Age-restricted videos always fail with Error 150 in YouTube IFrame embeds.
	if strings.EqualFold(v.ContentDetails.ContentRating.YtRating, "ytAgeRestricted") {
		return false
	}
	if rr := v.ContentDetails.RegionRestriction; rr != nil && regionCode != "" {
		for _, blocked := range rr.Blocked {
			if strings.EqualFold(blocked, regionCode) {
				return false
			}
		}
		if len(rr.Allowed) > 0 {
			allowedHere := false
			for _, allowed := range rr.Allowed {
				if strings.EqualFold(allowed, regionCode) {
					allowedHere = true
					break
				}
			}
			if !allowedHere {
				return false
			}
		}
	}
	return true
}

// embedRiskScore assigns lower scores to uploads less likely to carry silent
// Content ID / VEVO / Topic syndication blocks in embedded players.
func embedRiskScore(v youtubeVideoDetails) int {
	score := 0
	lowerTitle := strings.ToLower(v.Snippet.Title)
	lowerChannel := strings.ToLower(strings.TrimSpace(v.Snippet.ChannelTitle))

	if strings.Contains(lowerTitle, "lyrics") || strings.Contains(lowerTitle, "lyric") {
		score--
	}
	if strings.HasSuffix(lowerChannel, "- topic") {
		score += 3
	}
	if strings.HasSuffix(lowerChannel, "vevo") ||
		strings.Contains(lowerChannel, "universal music") ||
		strings.Contains(lowerChannel, "sony music") ||
		strings.Contains(lowerChannel, "warner music") {
		score += 3
	}
	if strings.Contains(lowerTitle, "official music video") ||
		strings.Contains(lowerTitle, "official video") ||
		strings.Contains(lowerTitle, "(music video)") ||
		strings.Contains(lowerTitle, "[music video]") {
		score += 2
	}
	return score
}

func (s *youtubeServer) rememberSong(song Song) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.cache) >= 512 {
		s.cache = map[string]cachedSong{}
	}
	s.cache[song.VideoID] = cachedSong{
		song:    song,
		expires: time.Now().Add(30 * time.Minute),
	}
}

func (s *youtubeServer) lookupCached(videoID string) (Song, bool) {
	s.mu.Lock()
	cached, ok := s.cache[videoID]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.song, true
	}
	return Song{}, false
}

func (s *youtubeServer) fetchVideoDetails(ctx context.Context, ids []string) ([]youtubeVideoDetails, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	endpoint := s.baseURL + "/youtube/v3/videos?" + url.Values{
		"part": {"snippet,status,contentDetails"},
		"id":   {strings.Join(ids, ",")},
		"key":  {s.apiKey},
	}.Encode()
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
		return nil, fmt.Errorf("youtube videos.list status %d", resp.StatusCode)
	}
	var parsed youtubeVideosResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed.Items, nil
}

func (s *youtubeServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}
	if s.apiKey == "" {
		http.Error(w, "missing YOUTUBE_API_KEY", http.StatusInternalServerError)
		return
	}

	searchURL := s.baseURL + "/youtube/v3/search?" + url.Values{
		"part":            {"snippet"},
		"type":            {"video"},
		"videoCategoryId": {"10"},
		"videoEmbeddable": {"true"},
		"videoSyndicated": {"true"},
		"regionCode":      {s.regionCode},
		"maxResults":      {"10"},
		"q":               {query},
		"key":             {s.apiKey},
	}.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, searchURL, nil)
	if err != nil {
		http.Error(w, "youtube request failed", http.StatusBadGateway)
		return
	}
	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "youtube request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "youtube returned an error", http.StatusBadGateway)
		return
	}

	var yt youtubeSearchResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<10)).Decode(&yt); err != nil {
		http.Error(w, "invalid youtube response", http.StatusBadGateway)
		return
	}

	ids := make([]string, 0, len(yt.Items))
	seen := make(map[string]bool, len(yt.Items))
	searchThumbs := make(map[string]string, len(yt.Items))
	for _, item := range yt.Items {
		id := item.ID.VideoID
		if !videoIDPattern.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		searchThumbs[id] = item.Snippet.Thumbnails.Default.URL
	}

	if len(ids) == 0 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode([]SearchResult{})
		return
	}

	details, err := s.fetchVideoDetails(r.Context(), ids)
	if err != nil {
		http.Error(w, "youtube verification failed", http.StatusBadGateway)
		return
	}

	byID := make(map[string]youtubeVideoDetails, len(details))
	for _, d := range details {
		byID[d.ID] = d
	}

	playable := make([]youtubeVideoDetails, 0, len(ids))
	for _, id := range ids {
		d, ok := byID[id]
		if !ok || !isPlayableInRegion(d, s.regionCode) {
			continue
		}
		playable = append(playable, d)
	}

	sort.SliceStable(playable, func(i, j int) bool {
		return embedRiskScore(playable[i]) < embedRiskScore(playable[j])
	})

	if len(playable) > 5 {
		playable = playable[:5]
	}

	results := make([]SearchResult, 0, len(playable))
	for _, d := range playable {
		thumb := d.Snippet.Thumbnails.Default.URL
		if thumb == "" {
			thumb = searchThumbs[d.ID]
		}
		results = append(results, SearchResult{
			VideoID:   d.ID,
			Title:     d.Snippet.Title,
			Channel:   d.Snippet.ChannelTitle,
			Thumbnail: thumb,
		})
		s.rememberSong(Song{
			VideoID: d.ID,
			Title:   d.Snippet.Title,
			Channel: d.Snippet.ChannelTitle,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func (s *youtubeServer) handleLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	videoID := strings.TrimSpace(r.URL.Query().Get("id"))
	if !videoIDPattern.MatchString(videoID) {
		http.Error(w, "invalid video ID", http.StatusBadRequest)
		return
	}
	if song, ok := s.lookupCached(videoID); ok {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(song)
		return
	}
	if s.apiKey == "" {
		http.Error(w, "missing YOUTUBE_API_KEY", http.StatusInternalServerError)
		return
	}

	song, err := s.lookupUpstream(r.Context(), videoID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(song)
}

func (s *youtubeServer) lookupUpstream(ctx context.Context, videoID string) (Song, error) {
	details, err := s.fetchVideoDetails(ctx, []string{videoID})
	if err != nil {
		return Song{}, errors.New("YouTube metadata lookup failed")
	}
	if len(details) != 1 || details[0].ID != videoID || !isPlayableInRegion(details[0], s.regionCode) {
		return Song{}, errors.New("video could not be verified for embedding")
	}
	song := Song{
		VideoID: details[0].ID,
		Title:   details[0].Snippet.Title,
		Channel: details[0].Snippet.ChannelTitle,
	}
	s.rememberSong(song)
	return song, nil
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func loadEnv() {
	for _, path := range []string{".env", "../.env", "../../.env"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k != "" && !strings.HasPrefix(k, "#") {
				if os.Getenv(k) == "" {
					_ = os.Setenv(k, v)
				}
			}
		}
	}
}

func main() {
	loadEnv()
	port := os.Getenv("PORT")
	if port == "" {
		port = "8081"
	}
	srv := newYouTubeServer()
	mux := http.NewServeMux()
	mux.HandleFunc("/search", srv.handleSearch)
	mux.HandleFunc("/lookup", srv.handleLookup)
	mux.HandleFunc("/healthz", handleHealthz)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
	}
	log.Println("YouTube Search API listening on :" + port)
	log.Fatal(server.ListenAndServe())
}
