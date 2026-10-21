package room

import "testing"

func TestGenreAndRequestOrder(t *testing.T) {
	if genre, err := CleanGenre(" classical "); err != nil || genre != "Classical" {
		t.Fatal(genre, err)
	}
	if _, err := CleanGenre("invented"); err == nil {
		t.Fatal("unknown genre accepted")
	}
	rm := NewRoom("host", "Host", func(string) bool { return false })
	first := &SongRequest{ID: "1", Song: Song{ID: "1", VideoID: "same"}, Status: Review}
	second := &SongRequest{ID: "2", Song: Song{ID: "2", VideoID: "same"}, Status: Allow}
	third := &SongRequest{ID: "3", Song: Song{ID: "3"}, Status: Block}
	rm.Requests = []*SongRequest{first, second, third}
	rm.ReleaseApproved()
	if len(rm.Queue) != 0 {
		t.Fatal("later approval passed an unresolved review")
	}
	if err := rm.DecideRequest("1", Allow); err != nil {
		t.Fatal(err)
	}
	if len(rm.Queue) != 2 || rm.Queue[0].ID != "1" || rm.Queue[1].ID != "2" {
		t.Fatalf("wrong order: %+v", rm.Queue)
	}
	rm.ReleaseApproved()
	if len(rm.Queue) != 2 {
		t.Fatal("duplicated admission")
	}
	if err := rm.DecideRequest("2", Block); err == nil {
		t.Fatal("already queued request changed")
	}
	if err := rm.DecideRequest("3", Allow); err != nil {
		t.Fatal(err)
	}
	if len(rm.Queue) != 3 || rm.Queue[2].ID != "3" {
		t.Fatal("override must go to the end")
	}
	if err := rm.DecideRequest("missing", Allow); err == nil {
		t.Fatal("unknown request accepted")
	}
	if err := rm.DecideRequest("1", "invalid"); err == nil {
		t.Fatal("invalid decision accepted")
	}
}
