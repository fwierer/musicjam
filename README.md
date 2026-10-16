# Music Jam

Music Jam is a shared listening app for parties and get-togethers. One **host** device (a laptop or TV) plays the music. **Guests** join on their phones by scanning a QR code, search YouTube, and add songs to a shared queue. No account is needed. An AI filter keeps the queue suited to the occasion, so nobody can sneak "Happy" into a funeral playlist.

## Tech stack

| Part | Choice |
|---|---|
| Frontend | HTML, Bootstrap 5, vanilla JS |
| Backend | Go (`net/http`) |
| Live data | In-memory for now, Redis planned (rooms, queues, cache with TTL) |
| Search / playback | YouTube Data API v3 and YouTube IFrame Player API |
| QR codes | qrcode.js |
| AI filter | Small hosted LLM (planned) |
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

## UI design (AI draft)

<img width="780" height="1688" alt="Guest search   pick (phone)" src="https://github.com/user-attachments/assets/400e6422-55b6-4929-b423-0fc453264a41" />
<img width="780" height="1688" alt="Guest session ended (phone)" src="https://github.com/user-attachments/assets/bbbd5baa-87c4-4ebf-95a6-2cca96225c24" />
<img width="2880" height="1800" alt="Host screen (laptop TV)" src="https://github.com/user-attachments/assets/c07d9dc4-0e9d-4afd-bdf3-fc39f6d326c8" />
<img width="2560" height="1600" alt="Start page (desktop)" src="https://github.com/user-attachments/assets/63a04029-7269-42d5-9d0a-66c6fc1fd4a3" />
