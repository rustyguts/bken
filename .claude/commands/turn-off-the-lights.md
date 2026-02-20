## BKEN Project Guide

This document serves as a guide for you to figure out areas that you can work on. Every so often, an agent will read this document and decide on its own what it should work on.

### Project goals

This is a client/server voice over ip application. Clients running the bken desktop app (located in client) will connect to a bken server (located in server). Users use their computer microphones to speak and communicate with other individuals connected to the same server as them. 

### Do Not Do

- Do not pick extremely difficult projects
- Do not try to rewrite the app in a big way or change fundamental technologies

### Workflow

- If there are uncommited git changes then commit them and push
- Work on your feature
- Write tests for your feature
- Run all tests and linting for the repo
- Commit and push
- Move the item to done

### Things to work on in no particular order

- Basic roles for the server. Owner / Member.
  - Owners can kick members from the server
  - Owners can create channels in the server and CRUD the channels
  - Owners can set the name of the server
- Server should have state. Recommend embedded sqlite database.
  - Database should have safe and reliable migrations that can grow over time
- Client should follow daisy ui for all UI styling
- Users should be able to move between channels
- Users should be able to connect to multiple servers and switch between them
- UI: The inteface should always remain simple, clean, modern
- UI: Beautiful settings page
- Voice: Echo cancellation feature, enabled by default
- Server owners should be able to generate invite links from the servers public endpoint. When openened in a browser this should open the app and automatically connect you to the server
- Servers should support chat rooms over WebTransport enabling live chat. Chats exist at the server level and also at the channel level (global chat and channel chat)
- UI: The UI should be modular and customizable. Certain elements should be movable. Users should be able to unlock the UI and then move panels around to suite their needs
- UI: Should be very responsive to small sizes all the way up to large desktop sizes.
- Performance is critical, analyze slow parts of the code and improve performance
- UI: Small icons can be uploaded and set per channel
- UI: A server icon can be uploaded and set
- Server: If possible, use the Echo web framework for server REST endpoints in addition to WebTransport
- Voice transmit speed and reliability are the single most important aspects of the application. It must be robust, handle errors, and be extremely fast.
- Code quality and readability
- Repo structure and organization

### Done

- UI: Users should be able to switch between all the different daisy UI themes
- Client should have a frameless GUI frame
- Client should have smooth transitions
- Client should also have state (JSON config file at ~/.config/bken/config.json)
- Reliable connection and disconnection between client and server
- Optimized Opus audio transmission rate based on connection speed to server
- Users can mute other users locally (client-side, no server involvement)
- Users hear notification tones for app events (connect, join, leave, mute, unmute)
- Voice: Automatic gain control (software AGC, enabled by default, configurable target level)
- Voice: Noise suppression enabled by default; all audio settings applied on startup (not just when settings panel opens)
- Voice: Ability to set volume (volume slider in settings panel)