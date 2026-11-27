package room

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DBURL points to the JSON Database service (e.g. http://db:8084).
// When empty, rooms are read from and written to DataDir on the local filesystem.
var DBURL string

var dbHTTPClient = &http.Client{Timeout: 3 * time.Second}

// ConfigureStore sets the persistence backend (remote DB_URL if non-empty, otherwise local DataDir).
func ConfigureStore(dbURL, dataDir string) {
	DBURL = strings.TrimRight(strings.TrimSpace(dbURL), "/")
	if dataDir != "" {
		DataDir = dataDir
	}
}

func saveRoomRemote(roomCode string, fileData []byte) error {
	endpoint := DBURL + "/rooms/" + url.PathEscape(roomCode)
	req, err := http.NewRequest(http.MethodPut, endpoint, bytes.NewReader(fileData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := dbHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("database service returned status %d", resp.StatusCode)
	}
	return nil
}

// LoadRooms loads all rooms from DBURL when configured, or from DataDir on local disk.
func LoadRooms() map[string]*Room {
	if DBURL == "" {
		return LoadExistingRoomsFromDatabase(DataDir)
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		loaded, err := loadRoomsRemote()
		if err == nil {
			return loaded
		}
		lastErr = err
		time.Sleep(300 * time.Millisecond)
	}
	log.Printf("failed to load rooms from database service %s: %v", DBURL, lastErr)
	return map[string]*Room{}
}

func loadRoomsRemote() (map[string]*Room, error) {
	resp, err := dbHTTPClient.Get(DBURL + "/rooms")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("database service returned status %d", resp.StatusCode)
	}

	var rawRooms []json.RawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&rawRooms); err != nil {
		return nil, err
	}

	activeRooms := make(map[string]*Room, len(rawRooms))
	for _, raw := range rawRooms {
		if rm, ok := decodeRoomJSON(raw, 0); ok {
			activeRooms[rm.roomCode] = rm
		}
	}
	return activeRooms, nil
}
