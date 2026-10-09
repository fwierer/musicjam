package main

import "github.com/FullStackKlerant/musicjam/backend/room"

func main() {
	nRoom := room.NewRoom()
	println(nRoom.Code())
}
