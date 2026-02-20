## BKEN Project Guide

This document serves as a guide for you to figure out areas that you can work on. Every so often, an agent will read this document and decide on its own what it should work on.

### Project goals

This is a client/server voice over ip application. Clients running the bken desktop app (located in client) will connect to a bken server (located in server). Users use their computer microphones to speak and communicate with other individuals connected to the same server as them. 

### Do Not Do

- Do not pick extremely difficult projects
- Do not try to rewrite the app in a big way or change fundamental technologies

### Workflow

- If there are uncommited git changes then commit them and push
- Work on your feature, use docker compose to run the stack
- Write tests for your feature
- Run all tests and linting for the repo
- Commit and push
- Move the item to done section of this file (.claude/commands/turn-off-the-lights.md)

### Things to work on

- Voice transmit speed and reliability are the single most important aspects of the application. It must be robust, handle errors, and be extremely fast.
- Server invite links - You could send these to people and they can join a server, would require external web service
### Done

- Rich link previews (server extracts first URL from chat, async fetches OpenGraph metadata with 4s timeout, broadcasts link_preview control message keyed by MsgID; client renders preview cards with image/title/description/site name; 13 server tests)

- Bug: All messages are shown regardless of the channel you are in. You should only see the chatroom messages for the channel that you are in (frontend filters messages by channel_id via visibleMessages computed property)
- Set a default global username. Right now users cant join a server unless their username is set. Generate one if one is not defined. Store the global username in the client state db
- Bug: I can hear voice from people in other channels. You should only receive voice packets for the channel that you are in
- Bug: Pressing disconnect stops voice but the UI does not update. It still shows that you are connected to the channel
- Differentiate the idea of being "Connected" to the server vs being connected via voice. When the user clicks on the server in the sidebar they are connected over WebTransport and start getting messages. The disconnect button only disconnects them from the voice channel that they are currently in. But they are still connected to the server itself so that they can chat, browse, do other things. Switching between other servers does truly disconnect and connect to another server instance
- Error states when you can't connect to the server or get disconnected (10s connect timeout, disconnect reason in ReconnectBanner + ServerChannels, transport cleanup on unexpected disconnect)
- Join voice button sometimes does not work (writeCtrl now returns errors so JoinChannel/ConnectVoice failures surface to frontend; StartReceiving cancels previous goroutine preventing duplicates; ConnectVoice cleans up audio on JoinChannel failure; frontend debounces rapid Join Voice clicks)
- Admin can create new channels (owner sees "+" button in channel panel header to create; right-click on any channel for rename/delete context menu; channel CRUD via WebTransport control messages with owner-only auth; deleted channels move users back to lobby; 9 new server tests)
- Admin can move users to a different channel (owner right-clicks user avatar in channel panel; context menu shows available channels; move_user control message with owner-only auth; 6 new server tests)
- File uploading in chatroom (10MB max; "+" button opens native file picker; drag & drop via Wails DragAndDrop; server HTTP upload/download endpoints with SQLite metadata + disk storage; image preview for image files, download link for others; file metadata relayed via control stream chat messages; 10 new tests across server, API, and store)
- Notifications when you miss messages (unread message count badges on channel tabs in chatroom and channel entries in sidebar; tracked per-channel in App.vue; incremented on incoming messages for non-viewed channels; cleared when user switches to view that channel)
- Jitter buffer with per-sender decoders, audio mixing, and Opus PLC (new client/internal/jitter package with 60ms reorder window; transport sends TaggedAudio with sender ID + seq; playback loop creates per-sender Opus decoders and mixes PCM additively; missing packets trigger Opus packet loss concealment; 12 jitter buffer tests)
- Opus inband FEC for better voice recovery under packet loss (encoder embeds low-bitrate copy of previous frame; jitter buffer peeks at next seq to provide FEC data on missing frames; playback uses DecodeFEC for higher-quality recovery than PLC; adaptive loop feeds measured loss % into encoder so FEC redundancy matches actual conditions; 7 new tests)
- Adaptive voice quality — DTX, EWMA loss smoothing, adaptive jitter buffer (Opus DTX enabled for bandwidth savings during silence; EWMA-smoothed loss estimation prevents FEC oscillation; inter-arrival jitter measurement in transport with RFC 3550-style EWMA; adaptive jitter buffer depth auto-tunes 1–8 frames based on measured jitter and loss; jitter_ms exposed in Metrics; 13 new tests across adapt, jitter, transport, and audio packages)
- Server-side circuit breaker for datagram fan-out (per-client sendHealth tracks consecutive SendDatagram failures; after 50 failures breaker opens and skips sends; periodic probe every 25 skips allows recovery; eliminates log spam from dead clients; skipped datagrams counted in metrics; 10 new tests for sendHealth unit + Broadcast integration)
- Resilient voice transmission with real-time quality feedback (client-side send loop tolerates transient errors with 50-failure circuit breaker instead of disconnecting on first error; dropped frame counters on capture/playback channels; quality_level classification in Metrics — good/moderate/poor based on loss, RTT, and jitter thresholds; backend pushes voice:quality events to frontend every 5s; MetricsBar with color-coded quality dot wired into UserControls; 10 new tests for quality levels, send threshold, and dropped frame tracking)
