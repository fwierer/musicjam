package room

import (
	"errors"
	"strings"
)

var Genres = []string{"Any", "Rock", "Pop", "Hip-Hop", "Electronic", "Classical", "Jazz", "Metal", "Reggae", "Country", "R&B"}

const (
	Pending = "pending"
	Allow   = "allow"
	Review  = "review"
	Block   = "block"
)

type SongRequest struct {
	ID          string `json:"id"`
	Song        Song   `json:"song"`
	UserID      string `json:"-"`
	RequestedBy string `json:"requestedBy"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Queued      bool   `json:"queued"`
}

func CleanGenre(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Any", nil
	}
	for _, genre := range Genres {
		if strings.EqualFold(value, genre) {
			return genre, nil
		}
	}
	return "", errors.New("unknown room genre")
}

func ValidDecision(value string) bool {
	return value == Allow || value == Review || value == Block
}

// Callers hold the backend mutex when changing requests or the queue.
func (room *Room) ReleaseApproved() {
	for room.nextRequest < len(room.Requests) {
		request := room.Requests[room.nextRequest]
		if request.Status == Pending || request.Status == Review {
			return
		}
		if request.Status == Allow && !request.Queued {
			room.Queue = append(room.Queue, request.Song)
			request.Queued = true
		}
		room.nextRequest++
	}
}

func (room *Room) Request(id string) *SongRequest {
	for _, request := range room.Requests {
		if request.ID == id {
			return request
		}
	}
	return nil
}

func (room *Room) DecideRequest(id, decision string) error {
	request := room.Request(id)
	if request == nil {
		return errors.New("song request not found")
	}
	if request.Status == Pending || request.Queued {
		return errors.New("song request cannot be changed")
	}
	if decision != Allow && decision != Block {
		return errors.New("invalid host decision")
	}
	previous := request.Status
	request.Status = decision
	request.Reason = "Decided by the host."
	// Overriding an already rejected request is a new admission at the end of the queue.
	if previous == Block && decision == Allow {
		for index, item := range room.Requests {
			if item == request && index < room.nextRequest {
				room.Queue = append(room.Queue, request.Song)
				request.Queued = true
				return nil
			}
		}
	}
	room.ReleaseApproved()
	return nil
}
