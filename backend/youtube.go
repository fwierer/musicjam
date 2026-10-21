package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

type youtubeSearchResponse struct {
	Items []youtubeSearchItem `json:"items"`
}

type youtubeSearchItem struct {
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
}

type searchResult struct {
	VideoID   string `json:"videoId"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
}

func handleYouTubeSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	apiKey := os.Getenv("YOUTUBE_API_KEY")
	if apiKey == "" {
		http.Error(w, "missing YOUTUBE_API_KEY", http.StatusInternalServerError)
		return
	}

	regionCode := os.Getenv("YOUTUBE_REGION")
	if regionCode == "" {
		regionCode = "IT"
	}

	searchURL := "https://www.googleapis.com/youtube/v3/search?" + url.Values{
		"part":            {"snippet"},
		"type":            {"video"},
		"videoCategoryId": {"10"},
		"videoEmbeddable": {"true"},
		"videoSyndicated": {"true"},
		"regionCode":      {regionCode},
		"maxResults":      {"5"},
		"q":               {query},
		"key":             {apiKey},
	}.Encode()

	request, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, searchURL, nil)
	resp, err := youtubeClient.Do(request)
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
	if err := json.NewDecoder(resp.Body).Decode(&yt); err != nil {
		http.Error(w, "invalid youtube response", http.StatusBadGateway)
		return
	}

	results := make([]searchResult, 0, len(yt.Items))
	for _, item := range yt.Items {
		if item.ID.VideoID == "" {
			continue
		}

		results = append(results, searchResult{
			VideoID:   item.ID.VideoID,
			Title:     item.Snippet.Title,
			Channel:   item.Snippet.ChannelTitle,
			Thumbnail: item.Snippet.Thumbnails.Default.URL,
		})
		rememberSong(room.Song{VideoID: item.ID.VideoID, Title: item.Snippet.Title, Channel: item.Snippet.ChannelTitle})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

var youtubeClient = &http.Client{Timeout: 10 * time.Second}
var trustedSongMu sync.Mutex

type cachedSong struct {
	song    room.Song
	expires time.Time
}

var trustedSongs = map[string]cachedSong{}

func rememberSong(song room.Song) {
	trustedSongMu.Lock()
	defer trustedSongMu.Unlock()
	if len(trustedSongs) >= 512 {
		trustedSongs = map[string]cachedSong{}
	}
	trustedSongs[song.VideoID] = cachedSong{song, time.Now().Add(30 * time.Minute)}
}

// Only server-fetched metadata is used as input to the genre classifier.
func lookupYouTubeSong(ctx context.Context, videoID string) (room.Song, error) {
	trustedSongMu.Lock()
	cached, ok := trustedSongs[videoID]
	trustedSongMu.Unlock()
	if ok && time.Now().Before(cached.expires) {
		return cached.song, nil
	}
	key := os.Getenv("YOUTUBE_API_KEY")
	if key == "" {
		return room.Song{}, errors.New("YouTube key unavailable")
	}
	endpoint := "https://www.googleapis.com/youtube/v3/videos?" + url.Values{
		"part": {"snippet,status"}, "id": {videoID}, "key": {key},
	}.Encode()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	response, err := youtubeClient.Do(request)
	if err != nil {
		return room.Song{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return room.Song{}, errors.New("YouTube metadata lookup failed")
	}
	var result struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title   string `json:"title"`
				Channel string `json:"channelTitle"`
			} `json:"snippet"`
			Status struct {
				Embeddable bool `json:"embeddable"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return room.Song{}, err
	}
	if len(result.Items) != 1 || result.Items[0].ID != videoID || !result.Items[0].Status.Embeddable || result.Items[0].Snippet.Title == "" {
		return room.Song{}, errors.New("video could not be verified for embedding")
	}
	item := result.Items[0]
	song := room.Song{VideoID: item.ID, Title: item.Snippet.Title, Channel: item.Snippet.Channel}
	rememberSong(song)
	return song, nil
}
