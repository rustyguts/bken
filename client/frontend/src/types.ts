/** Shared TypeScript interfaces for the bken frontend. */

/** A connected user in the voice room. */
export interface User {
  id: number
  username: string
  channel_id?: number // 0 or absent = lobby
}

/** A voice channel on the server. */
export interface Channel {
  id: number
  name: string
}

/** Payload emitted when a user joins. */
export interface UserJoinedEvent {
  id: number
  username: string
}

/** Payload emitted when a user leaves. */
export interface UserLeftEvent {
  id: number
}

/** Payload emitted when a user is speaking. */
export interface SpeakingEvent {
  id: number
}

/** A timestamped entry in the event log. */
export interface LogEvent {
  id: number
  time: string
  text: string
  type: 'join' | 'leave' | 'info'
}

/** Connection parameters submitted from the server browser form. */
export interface ConnectPayload {
  username: string
  addr: string
}

/** A single chat message received from the server. */
export interface ChatMessage {
  id: number         // client-side counter for v-for keys
  username: string
  message: string
  ts: number         // Unix ms timestamp (server-stamped)
  channelId: number  // 0 = server-wide, non-zero = channel-scoped
}
