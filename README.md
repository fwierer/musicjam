# Music Jam

Music Jam is a shared listening app for parties and get-togethers. One **host** device (a laptop or TV) plays the music. **Guests** join on their phones by scanning a QR code, search YouTube, and request songs for a shared queue. No account is needed. A local AI genre filter checks requests against the host's selected room genre, with uncertain requests reviewed by the host. Broader occasion-based moderation is planned.

## Tech stack

| Part | Choice |
|---|---|
| Frontend | HTML, Bootstrap 5, vanilla JS |
| Backend | Go (`net/http`) |
| Live data | In-memory for now, Redis planned (rooms, queues, cache with TTL) |
| Search / playback | YouTube Data API v3 and YouTube IFrame Player API |
| QR codes | qrcode.js |
| Genre filter | Local Ollama model, structured JSON verdicts |
| Hosting | Render or Railway (QR codes need HTTPS) |

## YouTube API

- **Search** goes through our backend (`/api/youtube/search`), which calls the YouTube Data API v3 `search` endpoint. This keeps the API key (`YOUTUBE_API_KEY`) on the server.
- Results are limited to embeddable, syndicated music videos (category 10) in the configured region (`YOUTUBE_REGION`, default `IT`), 5 results per search.
- **Playback** uses the YouTube IFrame Player API on the host page. When a song ends, the host moves the queue to the next song.
- **Quota:** about 100 searches per day, so we search only on submit and will cache results.
- The player has to stay visible, and extracting the audio is not allowed by YouTube's terms.

## Functional requirements

- The host creates a room and gets a room code and a QR code
- Guests join by QR code or room code, without an account
- Guests and the host search for songs on YouTube
- Everyone adds songs to a shared queue and sees it update
- Only the host device plays audio, and it plays the queue in order
- **AI filter:** when a room is created, the host picks an occasion. Every added song is checked against it: the server sends the title, artist and occasion to an LLM, which returns `allow`, `review` or `block`
  - `review` lets the host decide (also used for unknown songs or AI errors)
  - The host can always override the AI
  - Verdicts are cached per song and occasion
  - Song titles are untrusted input, so only the three verdict values are accepted (protection against prompt injection)
- **Session cleanup:** a room is deleted when the host ends it, when the host has been gone for more than 10 minutes, after 1 hour of inactivity, or after 24 hours at most

## Currently implemented

- **Hosting sessions:** the host creates a room with a random code and gets a QR code that links to the join page
- **Joining a session:** guests join by scanning the QR code or typing the room code. A cookie identifies each user
- **The queue:** everyone can add songs. The queue refreshes every 3 seconds, and only the host can advance it. A song is removed only if it is still the current one, so a delayed "ended" event can't skip the next song
- **YouTube player:** search through the backend, and playback on the host page that moves to the next song automatically
- **Room genre:** the host selects a genre when creating a room. `All genres` keeps the unfiltered behavior.
- **Local genre moderation:** filtered requests are checked with Ollama before admission. Only `allow` reaches the playable queue; `review` requires a host decision, and `block` is rejected. Both host and guests can request songs.
- **Host review:** the host can approve or reject uncertain requests and override rejections. An overridden rejection is appended at the end. Earlier unresolved requests hold later approvals so ordinary admission keeps request order.
- **Verified metadata:** the classifier receives server-fetched YouTube titles and channels, not the browser's submitted labels. Search results supply a short-lived metadata cache; other video IDs use `videos.list`.
- **Failure handling:** missing metadata, unavailable Ollama, timeouts, and invalid model output require host review. Classification runs outside the global room mutex. Decisions are cached for 30 minutes per model, genre, and verified song metadata.

## Run locally

Install [Go](https://go.dev/dl/) and [Ollama](https://ollama.com/download). The default local model is [qwen3:4b](https://ollama.com/library/qwen3:4b).

```powershell
ollama pull qwen3:4b
ollama serve
```

If the Ollama desktop application is already running, a separate `ollama serve` is unnecessary. In a second terminal, from `backend`:

```powershell
$env:YOUTUBE_API_KEY = "your-youtube-api-key"
# Optional settings (these are the defaults):
$env:OLLAMA_URL = "http://127.0.0.1:11434"
$env:OLLAMA_MODEL = "qwen3:4b"
go run .
```

Open `http://localhost:8080`, enter your name, choose a room genre, and create a jam. Guests enter their name and join using the room code or QR link. Start playback once on the host device. Pending and rejected requests appear separately from the playable queue; the host decides reviews under **Song requests**.

Genre classification uses text metadata and the model's knowledge, not audio analysis. It can be wrong, particularly for obscure songs, covers, and remixes. Missing local setup never silently approves a filtered song. No OpenAI key is used. The broader occasion-based AI filter and automatic session cleanup described above are still planned.

The local Ollama request uses [structured outputs](https://docs.ollama.com/capabilities/structured-outputs); the backend additionally validates the returned decision and reason.

```powershell
# In backend, without Ollama or a real YouTube key:
go test ./...
```

## UI design (AI draft)

<img width="780" height="1688" alt="Guest search   pick (phone)" src="https://github.com/user-attachments/assets/400e6422-55b6-4929-b423-0fc453264a41" />
<img width="780" height="1688" alt="Guest session ended (phone)" src="https://github.com/user-attachments/assets/bbbd5baa-87c4-4ebf-95a6-2cca96225c24" />
<img width="2880" height="1800" alt="Host screen (laptop TV)" src="https://github.com/user-attachments/assets/c07d9dc4-0e9d-4afd-bdf3-fc39f6d326c8" />
<img width="2560" height="1600" alt="Start page (desktop)" src="https://github.com/user-attachments/assets/63a04029-7269-42d5-9d0a-66c6fc1fd4a3" />
