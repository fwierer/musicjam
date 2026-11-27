package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

// --- Moderation (LLM) Bounded Context Client ---

type verdict struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

type songClassifier interface {
	Classify(context.Context, room.Song, string) (verdict, error)
}

type llmClient struct {
	baseURL string
	client  *http.Client
}

func newLLMClient() *llmClient {
	return &llmClient{
		baseURL: strings.TrimRight(os.Getenv("LLM_URL"), "/"),
		client:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *llmClient) endpoint() string {
	if env := strings.TrimRight(os.Getenv("LLM_URL"), "/"); env != "" {
		return env
	}
	if c.baseURL != "" {
		return c.baseURL
	}
	return "http://127.0.0.1:8082"
}

func (c *llmClient) Classify(ctx context.Context, song room.Song, theme string) (verdict, error) {
	body, err := json.Marshal(struct {
		Song  room.Song `json:"song"`
		Theme string    `json:"theme"`
	}{Song: song, Theme: theme})
	if err != nil {
		return verdict{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint()+"/moderate", bytes.NewReader(body))
	if err != nil {
		return verdict{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return verdict{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return verdict{}, fmt.Errorf("LLM service status %d", resp.StatusCode)
	}

	var result struct {
		Song     room.Song `json:"song"`
		Decision string    `json:"decision"`
		Reason   string    `json:"reason"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return verdict{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return verdict{}, errors.New("extra response output")
	}

	reason := strings.TrimSpace(result.Reason)
	switch result.Decision {
	case "yes":
		return verdict{Decision: room.Allow, Reason: ""}, nil
	case "no":
		if reason == "" || utf8.RuneCountInString(reason) > 400 {
			return verdict{}, errors.New("invalid rejection reason")
		}
		return verdict{Decision: room.Block, Reason: reason}, nil
	default:
		return verdict{}, errors.New("invalid LLM decision")
	}
}

// --- Search Catalog (YouTube) Bounded Context Client ---

type searchResult struct {
	VideoID   string `json:"videoId"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
	Thumbnail string `json:"thumbnail"`
}

type youtubeServiceClient struct {
	baseURL string
	client  *http.Client
}

func newYouTubeServiceClient() *youtubeServiceClient {
	return &youtubeServiceClient{
		baseURL: strings.TrimRight(os.Getenv("YOUTUBE_URL"), "/"),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *youtubeServiceClient) endpoint() string {
	if env := strings.TrimRight(os.Getenv("YOUTUBE_URL"), "/"); env != "" {
		return env
	}
	if c.baseURL != "" {
		return c.baseURL
	}
	return "http://127.0.0.1:8081"
}

func (c *youtubeServiceClient) Search(ctx context.Context, query string) ([]searchResult, error) {
	reqURL := c.endpoint() + "/search?q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube service status %d", resp.StatusCode)
	}

	var results []searchResult
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 128<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&results); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("extra response output")
	}
	return results, nil
}

func (c *youtubeServiceClient) Lookup(ctx context.Context, videoID string) (room.Song, error) {
	reqURL := c.endpoint() + "/lookup?id=" + url.QueryEscape(videoID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return room.Song{}, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return room.Song{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return room.Song{}, fmt.Errorf("youtube lookup status %d", resp.StatusCode)
	}

	var song room.Song
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&song); err != nil {
		return room.Song{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return room.Song{}, errors.New("extra response output")
	}
	if song.VideoID != videoID || strings.TrimSpace(song.Title) == "" {
		return room.Song{}, errors.New("invalid verified song metadata")
	}
	return song, nil
}

var youtubeService = newYouTubeServiceClient()

func handleYouTubeSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Error(w, "missing query", http.StatusBadRequest)
		return
	}

	results, err := youtubeService.Search(r.Context(), query)
	if err != nil {
		http.Error(w, "youtube search unavailable", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func lookupYouTubeSong(ctx context.Context, videoID string) (room.Song, error) {
	return youtubeService.Lookup(ctx, videoID)
}
