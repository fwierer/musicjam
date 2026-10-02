# Music Jam 
 
Shared listening sessions: one **host** device plays music, **guests** join by QR code on their phones, search YouTube, and add songs to a shared queue.
 
## Features
 
- Host creates a room and gets a room code plus QR code
- Guests join by QR code, no account needed
- Search songs via YouTube, shared real-time queue
- Only the host device plays audio
- AI filter blocks songs that don't fit the occasion (e.g. "Happy" at a funeral)
## Tech stack
 
| Part | Choice |
|---|---|
| Frontend | HTML, Bootstrap, a little vanilla JS |
| Backend | Go + WebSockets |
| Live data | Redis (rooms, queues, cache, with TTL) |
| Persistent data | PostgreSQL (history, stats, later) |
| Search / playback | YouTube Data API v3 / YouTube IFrame Player API |
| AI filter | Small hosted LLM |
| Hosting | Render or Railway (needs HTTPS for QR codes) |
 
## How it works
 
1. Host starts a jam and picks the occasion. The server creates a room with a code and a secret host token.
2. Guests scan the QR code (`/join/<code>`) and connect via WebSocket.
3. Searches go through our server, which calls YouTube and caches the results.
4. Each added song is checked by the AI filter, then broadcast to everyone.
5. The host page plays the queue and moves to the next song when one ends.
## AI song filter
 
The server sends the song title, artist, and room occasion to an LLM, which returns JSON:
`{"verdict": "allow" | "review" | "block", "reason": "..."}`
 
- **review:** the host decides (also used for unknown songs or AI errors)
- The host can always override the AI
- Verdicts are cached in Redis per song + occasion
- Song titles are untrusted input, so we only accept the three verdict values (prompt injection protection)
## Session cleanup
 
A room is deleted when the host ends it, when the host has been gone longer than 10 minutes, after 1 hour of inactivity, or after 24 hours max.
 
## Important limits
 
- YouTube quota is about **100 searches per day**, so cache results and search only on submit
- The YouTube player must stay visible, and audio extraction is not allowed
- Use a laptop or PC as the host, since phones stop playback when the screen turns off
 
