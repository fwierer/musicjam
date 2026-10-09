package main

type Room struct {
	ID    string
	Queue []Song
}

type Song struct {
	VideoID string `json:"videoId"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
}

func createRoom(id string) Room {
	return Room{
		ID:    id,
		Queue: []Song{},
	}
}
