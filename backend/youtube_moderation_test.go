package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestYouTubeMetadataLookup(t *testing.T) {
	previousClient, previousSongs := youtubeClient, trustedSongs
	t.Cleanup(func() { youtubeClient, trustedSongs = previousClient, previousSongs })
	trustedSongs = map[string]cachedSong{}
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	calls := 0
	youtubeClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Path != "/youtube/v3/videos" || r.URL.Query().Get("id") != "zzzzzzzzzzz" || r.URL.Query().Get("part") != "snippet,status" {
			t.Error("wrong metadata lookup", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"items":[{"id":"zzzzzzzzzzz","snippet":{"title":"Canonical song","channelTitle":"Canonical artist"},"status":{"embeddable":true}}]}`)), Header: make(http.Header)}, nil
	})}
	for range 2 {
		song, err := lookupYouTubeSong(context.Background(), "zzzzzzzzzzz")
		if err != nil || song.Title != "Canonical song" || song.Channel != "Canonical artist" {
			t.Fatal(song, err)
		}
	}
	if calls != 1 {
		t.Fatal("verified metadata was not cached")
	}
}
