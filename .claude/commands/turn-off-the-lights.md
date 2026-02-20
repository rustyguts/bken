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

- Set a default global username. Right now users cant join a server unless their username is set. Generate one if one is not defined. Store the global username in the client state db
- Bug: I can hear voice from people in other channels. You should only receive voice packets for the channel that you are in
- Bug: Pressing disconnect stops voice but the UI does not update. It still shows that you are connected to the channel
- Bug: All messages are shown regardless of the channel you are in. You should only see the chatroom messages for the channel that you are in
- Error states when you can't connect to the server or get disconnected
- Join voice button sometimes does not work.
- Voice transmit speed and reliability are the single most important aspects of the application. It must be robust, handle errors, and be extremely fast.
- Server invite links - You could send these to people and they can join a server, would require external web service
- File uploading in chatroom. Max size 10mb. Drag and drop should work. Or a Plus button with droptop (open top) and "Upload file"
- Rich link previews
- Notifications when you miss messages
- Admin can drag users into a different channel (or right click and move them)
- Admin can create new channels (Right click on server in sidebar create channel)
- Differentiate the idea of being "Connected" to the server vs being connected via voice. When the user clicks on the server in the sidebar they are connected over WebTransport and start getting messages. The disconnect button only disconnects them from the voice channel that they are currently in. But they are still connected to the server itself so that they can chat, browse, do other things. Switching between other servers does truly disconnect and connect to another server instance

### Done

