package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
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
		"part":             {"snippet"},
		"type":             {"video"},
		"videoCategoryId":  {"10"},
		"videoEmbeddable":  {"true"},
		"videoSyndicated":  {"true"},
		"regionCode":       {regionCode},
		"maxResults":       {"5"},
		"q":                {query},
		"key":              {apiKey},
	}.Encode()

	resp, err := http.Get(searchURL)
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
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
