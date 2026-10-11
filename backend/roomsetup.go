package main

import "github.com/FullStackKlerant/musicjam/backend/room"

// createRoom creates a new room and prints its code
func createRoom() *room.Room {
	nRoom := room.NewRoom()
	println(nRoom.Code())
	return nRoom
}
