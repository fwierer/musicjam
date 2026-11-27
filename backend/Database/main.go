package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var roomCodePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type dbServer struct {
	dataDir string
	mu      sync.RWMutex
}

func newDBServer(dataDir string) *dbServer {
	return &dbServer{dataDir: dataDir}
}

func (s *dbServer) roomFilePath(code string) string {
	return filepath.Join(s.dataDir, "room_"+code+".json")
}

func (s *dbServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *dbServer) handleRooms(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[]"))
			return
		}
		http.Error(w, "failed to read database directory", http.StatusInternalServerError)
		return
	}

	rooms := make([]json.RawMessage, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "room_") || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dataDir, entry.Name()))
		if err != nil || !json.Valid(data) {
			continue
		}
		rooms = append(rooms, json.RawMessage(data))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rooms)
}

func (s *dbServer) handleRoomByCode(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimPrefix(r.URL.Path, "/rooms/")
	if !roomCodePattern.MatchString(code) {
		http.Error(w, "invalid room code", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		data, err := os.ReadFile(s.roomFilePath(code))
		s.mu.RUnlock()
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "room not found", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to read room", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)

	case http.MethodPut:
		data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil || !json.Valid(data) {
			http.Error(w, "invalid room JSON", http.StatusBadRequest)
			return
		}
		s.mu.Lock()
		target := s.roomFilePath(code)
		tmp := target + ".tmp"
		err = os.WriteFile(tmp, data, 0644)
		if err == nil {
			err = os.Rename(tmp, target)
		}
		s.mu.Unlock()
		if err != nil {
			http.Error(w, "failed to save room", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"saved"}`))

	case http.MethodDelete:
		s.mu.Lock()
		err := os.Remove(s.roomFilePath(code))
		s.mu.Unlock()
		if err != nil && !os.IsNotExist(err) {
			http.Error(w, "failed to delete room", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"deleted"}`))

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func registerRoutes(mux *http.ServeMux, srv *dbServer) {
	mux.HandleFunc("/healthz", srv.handleHealthz)
	mux.HandleFunc("/rooms", srv.handleRooms)
	mux.HandleFunc("/rooms/", srv.handleRoomByCode)
}

func main() {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8084"
	}
	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "../../database"
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		log.Fatalf("failed to create DATA_DIR %q: %v", dataDir, err)
	}

	srv := newDBServer(dataDir)
	mux := http.NewServeMux()
	registerRoutes(mux, srv)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	log.Printf("JSON Database API listening on :%s (dir: %s)", port, dataDir)
	log.Fatal(server.ListenAndServe())
}
