/** Shared TypeScript interfaces for the bken frontend. */

/** A connected user in the voice channel. */
export interface User {
  id: number
  username: string
  channel_id?: number // the channel the user is currently in
  role?: 'OWNER' | 'ADMIN' | 'MODERATOR' | 'USER'
}

/** A voice channel on the server. */
export interface Channel {
  id: number
  name: string
  max_users?: number // 0 or absent = unlimited
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

/** Connection parameters submitted from the server browser form. */
export interface ConnectPayload {
  username: string
  addr: string
}

/** Rich link preview metadata fetched by the server. */
export interface LinkPreview {
  url: string
  title: string
  description: string
  image: string
  siteName: string
}

/** Video state for a user (camera or screen share). */
export interface VideoState {
  active: boolean
  screenShare: boolean
  layers?: VideoLayer[] // available simulcast layers
}

/** A simulcast video layer describing resolution and bitrate. */
export interface VideoLayer {
  quality: string // "high", "medium", or "low"
  width: number
  height: number
  bitrate: number // kbps
}

/** Describes a single emoji reaction on a message. */
export interface ReactionInfo {
  emoji: string
  user_ids: number[]
  count: number
}

/** Preview of the original message in a reply. */
export interface ReplyPreview {
  msg_id: number
  username: string
  message: string
  deleted?: boolean
}

/** A search result from the server. */
export interface SearchResult {
  msg_id: number
  username: string
  message: string
  ts: number
  channel_id: number
}

/** A pinned message. */
export interface PinnedMsg {
  msg_id: number
  username: string
  message: string
  ts: number
  pinned_by: number
}

/** A single chat message received from the server. */
export interface ChatMessage {
  id: number         // client-side counter for v-for keys
  msgId: number      // server-assigned message ID (for matching link previews)
  senderId: number   // server-assigned sender user ID (for edit/delete authorisation)
  username: string
  message: string
  ts: number         // Unix ms timestamp (server-stamped)
  channelId: number  // the channel this message belongs to
  fileId?: number    // uploaded file DB id
  fileName?: string  // original filename
  fileSize?: number  // file size in bytes
  fileUrl?: string   // download URL (constructed by Go layer)
  linkPreview?: LinkPreview // rich link preview (populated asynchronously)
  edited?: boolean   // true if the message has been edited
  editedTs?: number  // Unix ms timestamp of the last edit
  deleted?: boolean  // true if the message has been deleted
  system?: boolean   // true if this is a system event message (join/leave/kick/etc.)
  mentions?: number[] // user IDs mentioned via @DisplayName
  reactions?: ReactionInfo[] // emoji reactions on this message
  replyTo?: number   // message ID being replied to
  replyPreview?: ReplyPreview // preview of the replied-to message
  pinned?: boolean   // true if the message is pinned
}
