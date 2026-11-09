package room

import (
	"errors"
	"strings"
	"testing"
)

func TestGenreAndRequestOrder(t *testing.T) {
	oldDir := DataDir
	t.Cleanup(func() { DataDir = oldDir })
	DataDir = t.TempDir()

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

func TestRoomPersistenceAndMembersUnit(t *testing.T) {
	oldDir := DataDir
	t.Cleanup(func() { DataDir = oldDir })
	DataDir = t.TempDir()

	if _, err := CleanName("   "); !errors.Is(err, ErrNameRequired) {
		t.Fatalf("expected ErrNameRequired, got %v", err)
	}
	if _, err := CleanName(strings.Repeat("a", MaxNameLength+1)); !errors.Is(err, ErrNameTooLong) {
		t.Fatalf("expected ErrNameTooLong, got %v", err)
	}
	if name, err := CleanRoomName("", "Alex"); err != nil || name != "Alex's Jam" {
		t.Fatalf("expected default room name, got %q (%v)", name, err)
	}
	if _, err := CleanRoomName(strings.Repeat("x", MaxRoomNameLength+1), "Alex"); !errors.Is(err, ErrRoomNameTooLong) {
		t.Fatalf("expected ErrRoomNameTooLong, got %v", err)
	}

	rm := NewRoom("host-uid", "Host", func(string) bool { return false })
	guest := rm.AddMember("guest-uid", "Guest")
	rm.Queue = append(rm.Queue, Song{ID: "s1", VideoID: "v1", Title: "Song 1", Channel: "Chan"})
	rm.Save()

	if !strings.Contains(rm.String(), "Song 1 - Chan") {
		t.Fatalf("unexpected String() output: %s", rm.String())
	}

	loaded := LoadExistingRoomsFromDatabase(DataDir)
	restored := loaded[rm.Code()]
	if restored == nil {
		t.Fatal("room was not loaded from disk")
	}
	if m, ok := restored.Member("guest-uid"); !ok || m.UserID != "guest-uid" || m.ID != guest.ID {
		t.Fatalf("guest member not properly restored: %+v", m)
	}
	if len(restored.Members()) != 2 {
		t.Fatalf("expected 2 members, got %d", len(restored.Members()))
	}
	if _, err := restored.RemoveMember(restored.MemberList()[0].ID); !errors.Is(err, ErrRemoveHost) {
		t.Fatalf("expected ErrRemoveHost, got %v", err)
	}
	if _, err := restored.RemoveMember(guest.ID); err != nil {
		t.Fatalf("unexpected error removing guest: %v", err)
	}
}
