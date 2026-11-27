package main

import (
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/FullStackKlerant/musicjam/backend/room"
)

// mu guards rooms, sessions and the rooms' contents, since every request runs on its own goroutine
var mu sync.Mutex
var rooms = map[string]*room.Room{}
var sessions = map[string]*Session{} // key: user ID from the cookie

type Session struct {
	Name     string `json:"name"`
	RoomCode string `json:"roomCode"`
	IsHost   bool   `json:"isHost"`
}

func main() {
	loadEnv()

	mux := http.NewServeMux()
	registerHandlers(mux)
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "../database"
	}
	room.ConfigureStore(os.Getenv("DB_URL"), dataDir)
	if room.DBURL == "" {
		_ = os.MkdirAll(room.DataDir, 0755)
	}
	rooms = room.LoadRooms()
	sessions = loadSessionsFromRooms(rooms)

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8000"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	if origin := getJoinOrigin(nil); !isLoopbackOrigin(origin) {
		log.Println("MusicJam running on " + origin)
	} else {
		log.Printf("MusicJam listening on 0.0.0.0:%s (open via http://<YOUR_LAN_IP>:%s on your network)", port, port)
	}
	log.Fatal(server.ListenAndServe())
}

// loadSessionsFromRooms rebuilds active user sessions directly from the members
// stored inside each room's JSON file. Rooms are processed from oldest to newest
// so a user who joined a newer room gets their latest room session.
func loadSessionsFromRooms(loadedRooms map[string]*room.Room) map[string]*Session {
	loaded := make(map[string]*Session)
	roomList := make([]*room.Room, 0, len(loadedRooms))
	for _, rm := range loadedRooms {
		roomList = append(roomList, rm)
	}
	sort.Slice(roomList, func(i, j int) bool {
		if roomList[i].UpdatedAt != roomList[j].UpdatedAt {
			return roomList[i].UpdatedAt < roomList[j].UpdatedAt
		}
		return roomList[i].Code() < roomList[j].Code()
	})
	for _, rm := range roomList {
		for userID, m := range rm.Members() {
			loaded[userID] = &Session{
				Name:     m.Name,
				RoomCode: rm.Code(),
				IsHost:   rm.IsHost(userID),
			}
		}
		for _, req := range rm.Requests {
			if req.Status == room.Pending && !rm.ProcessingRequests {
				rm.ProcessingRequests = true
				go processSongRequests(rm, classifier, verifySong)
				break
			}
		}
	}
	return loaded
}

func isRunningInDocker() bool {
	if os.Getenv("RUNNING_IN_DOCKER") == "1" {
		return true
	}
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

func isLoopbackHost(hostPort string) bool {
	h := strings.TrimSpace(hostPort)
	if h == "" {
		return true
	}
	if splitHost, _, err := net.SplitHostPort(h); err == nil {
		h = splitHost
	} else {
		h = strings.Trim(h, "[]")
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsUnspecified()
	}
	return false
}

func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return true
	}
	return isLoopbackHost(u.Host)
}

func isVirtualBridgeInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"docker", "br-", "veth", "virbr", "cni", "flannel"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// GetLocalIP returns the local preferred LAN IPv4 address used to host the backend
// so other devices on the same network can connect to the service.
func GetLocalIP() net.IP {
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		defer conn.Close()
		if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok && udpAddr.IP != nil && !udpAddr.IP.IsLoopback() {
			if v4 := udpAddr.IP.To4(); v4 != nil {
				return v4
			}
			return udpAddr.IP
		}
	}

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isVirtualBridgeInterface(iface.Name) {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				var ip net.IP
				switch v := addr.(type) {
				case *net.IPNet:
					ip = v.IP
				case *net.IPAddr:
					ip = v.IP
				}
				if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
					continue
				}
				if v4 := ip.To4(); v4 != nil {
					return v4
				}
			}
		}
	}

	return net.IPv4(127, 0, 0, 1)
}

func requestPort(r *http.Request) string {
	if r != nil && r.Host != "" {
		if _, p, err := net.SplitHostPort(r.Host); err == nil && p != "" {
			return p
		}
	}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		return p
	}
	return "8000"
}

func requestScheme(r *http.Request) string {
	if r != nil {
		if proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); proto != "" {
			return proto
		}
		if r.TLS != nil {
			return "https"
		}
	}
	return "http"
}

// getJoinOrigin determines the network-reachable origin URL encoded into QR codes.
// Priority:
//  1. Explicit non-loopback PUBLIC_ORIGIN env var (e.g. https://jam.example.com or http://192.168.1.42:8000)
//  2. Explicit non-loopback HOST_IP env var (e.g. 192.168.1.42)
//  3. Non-loopback Host header from the current HTTP request (when accessed via http://<LAN_IP>:8000)
//  4. Local LAN IPv4 auto-detected via GetLocalIP() when running outside an isolated Docker bridge
func getJoinOrigin(r *http.Request) string {
	if origin := strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_ORIGIN")), "/"); origin != "" {
		if !isLoopbackOrigin(origin) {
			return origin
		}
	}

	port := requestPort(r)
	if hostIP := strings.TrimSpace(os.Getenv("HOST_IP")); hostIP != "" && !isLoopbackHost(hostIP) {
		if _, _, err := net.SplitHostPort(hostIP); err == nil {
			return "http://" + hostIP
		}
		return "http://" + net.JoinHostPort(hostIP, port)
	}

	if r != nil {
		reqHost := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
		if reqHost == "" {
			reqHost = strings.TrimSpace(r.Host)
		}
		if reqHost != "" && !isLoopbackHost(reqHost) {
			return requestScheme(r) + "://" + reqHost
		}
	}

	if !isRunningInDocker() {
		if ip := GetLocalIP(); !ip.IsLoopback() {
			return "http://" + net.JoinHostPort(ip.String(), port)
		}
	}

	return "http://localhost:" + port
}

func loadEnv() {
	for _, path := range []string{".env", "../.env"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k != "" && !strings.HasPrefix(k, "#") {
				if os.Getenv(k) == "" {
					_ = os.Setenv(k, v)
				}
			}
		}
	}
}
