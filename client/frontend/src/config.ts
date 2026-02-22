// Config bindings — thin wrappers over the auto-generated Wails bridge.
// In browser mode (no Wails), a BrowserTransport provides the same API
// via WebSocket, with globals installed so wailsjs imports work too.

import { BrowserTransport, BrowserEventBus } from './browser-transport'

export interface ServerEntry {
  name: string
  addr: string
}

export type MessageDensity = 'compact' | 'default' | 'comfortable'

export interface Config {
  theme: string
  theme_mode?: string
  username: string
  input_device_id: number
  output_device_id: number
  volume: number
  audio_bitrate_kbps: number
  noise_enabled: boolean
  aec_enabled: boolean
  agc_enabled: boolean
  ptt_enabled: boolean
  ptt_key: string
  servers: ServerEntry[]
  message_density?: MessageDensity
  show_system_messages?: boolean
  // Legacy fields persisted by older builds. Kept optional for compatibility.
  noise_level?: number
  agc_level?: number
}

/* eslint-disable @typescript-eslint/no-explicit-any */

// Detect if we're running inside Wails or in a plain browser.
const isWails = !!(window as any)?.go?.main?.App

// In browser mode, install window.go.main.App and window.runtime globals
// so that all wailsjs imports work transparently.
if (!isWails) {
  const eventBus = new BrowserEventBus()
  const transport = new BrowserTransport(eventBus)
  const eb = transport.eventBus
  ;(window as any).go = { main: { App: transport.bridgeObject() } }
  ;(window as any).runtime = {
    EventsOn: (n: string, cb: (...args: any[]) => void) => eb.EventsOn(n, cb),
    EventsOnMultiple: (n: string, cb: (...args: any[]) => void, max: number) =>
      eb.EventsOnMultiple(n, cb, max),
    EventsOnce: (n: string, cb: (...args: any[]) => void) =>
      eb.EventsOnce(n, cb),
    EventsOff: (n: string, ...names: string[]) => eb.EventsOff(n, ...names),
    EventsOffAll: () => eb.EventsOffAll(),
    EventsEmit: (n: string, ...data: any[]) => eb.EventsEmit(n, ...data),
    // Stubs for window.runtime methods used by wailsjs/runtime/runtime.js
    LogPrint: () => {},
    LogTrace: () => {},
    LogDebug: () => {},
    LogInfo: () => {},
    LogWarning: () => {},
    LogError: () => {},
    LogFatal: () => {},
    WindowReload: () => location.reload(),
    WindowReloadApp: () => location.reload(),
    WindowSetAlwaysOnTop: () => {},
    WindowSetSystemDefaultTheme: () => {},
    WindowSetLightTheme: () => {},
    WindowSetDarkTheme: () => {},
    WindowCenter: () => {},
    WindowSetTitle: (t: string) => {
      document.title = t
    },
    WindowFullscreen: () => {},
    WindowUnfullscreen: () => {},
    WindowIsFullscreen: () => Promise.resolve(false),
    WindowGetSize: () => Promise.resolve({ w: window.innerWidth, h: window.innerHeight }),
    WindowSetSize: () => {},
    WindowSetMaxSize: () => {},
    WindowSetMinSize: () => {},
    WindowSetPosition: () => {},
    WindowGetPosition: () => Promise.resolve({ x: 0, y: 0 }),
    WindowHide: () => {},
    WindowShow: () => {},
    WindowMaximise: () => {},
    WindowToggleMaximise: () => {},
    WindowUnmaximise: () => {},
    WindowIsMaximised: () => Promise.resolve(false),
    WindowMinimise: () => {},
    WindowUnminimise: () => {},
    WindowIsMinimised: () => Promise.resolve(false),
    WindowIsNormal: () => Promise.resolve(true),
    WindowSetBackgroundColour: () => {},
    ScreenGetAll: () => Promise.resolve([]),
    BrowserOpenURL: (url: string) => window.open(url),
    Environment: () =>
      Promise.resolve({ buildType: 'browser', platform: navigator.platform, arch: '' }),
    Quit: () => {},
    Hide: () => {},
    Show: () => {},
    ClipboardGetText: () => Promise.resolve(''),
    ClipboardSetText: () => Promise.resolve(true),
    OnFileDrop: () => {},
    OnFileDropOff: () => {},
    CanResolveFilePaths: () => false,
    ResolveFilePaths: () => {},
  }
}

const bridge = () => (window as any)['go']['main']['App']

// --- Exports previously from wailsjs/go/main/App ---

export function Connect(addr: string, username: string): Promise<string> {
  return bridge()['Connect'](addr, username)
}

export function Disconnect(): Promise<void> {
  return bridge()['Disconnect']()
}

export function DisconnectVoice(): Promise<string> {
  return bridge()['DisconnectVoice']()
}

export function GetAutoLogin(): Promise<{ username: string; addr: string }> {
  return bridge()['GetAutoLogin']()
}

// --- Exports previously from wailsjs/runtime/runtime ---

export function EventsOn(eventName: string, callback: (...data: any[]) => void): () => void {
  return (window as any).runtime.EventsOn(eventName, callback)
}

export function EventsOff(eventName: string, ...additionalEventNames: string[]): void {
  ;(window as any).runtime.EventsOff(eventName, ...additionalEventNames)
}

// --- Config bindings ---

export function ApplyConfig(): Promise<void> {
  return bridge()['ApplyConfig']()
}

export function GetConfig(): Promise<Config> {
  return bridge()['GetConfig']()
}

export function SaveConfig(cfg: Config): Promise<void> {
  return bridge()['SaveConfig'](cfg)
}

// --- AEC bindings ---

export function SetAEC(enabled: boolean): Promise<void> {
  return bridge()['SetAEC'](enabled)
}

// --- AGC bindings ---

export function SetAGC(enabled: boolean): Promise<void> {
  return bridge()['SetAGC'](enabled)
}

// --- Audio bitrate bindings ---

export function SetAudioBitrate(kbps: number): Promise<void> {
  return bridge()['SetAudioBitrate'](kbps)
}

export function GetAudioBitrate(): Promise<number> {
  return bridge()['GetAudioBitrate']()
}

// --- Input Level bindings ---

export function GetInputLevel(): Promise<number> {
  return bridge()['GetInputLevel']()
}

// --- Notification Volume bindings ---

export function SetNotificationVolume(volume: number): Promise<void> {
  return bridge()['SetNotificationVolume'](volume)
}

export function GetNotificationVolume(): Promise<number> {
  return bridge()['GetNotificationVolume']()
}

// --- PTT bindings ---

export function SetPTTMode(enabled: boolean): Promise<void> {
  return bridge()['SetPTTMode'](enabled)
}

export function PTTKeyDown(): Promise<void> {
  return bridge()['PTTKeyDown']()
}

export function PTTKeyUp(): Promise<void> {
  return bridge()['PTTKeyUp']()
}

// --- Per-user local mute bindings ---

export function MuteUser(id: number): Promise<void> {
  return bridge()['MuteUser'](id)
}

export function UnmuteUser(id: number): Promise<void> {
  return bridge()['UnmuteUser'](id)
}

export function GetMutedUsers(): Promise<number[]> {
  return bridge()['GetMutedUsers']()
}

// --- Per-user volume bindings ---

export function SetUserVolume(userID: number, volume: number): Promise<void> {
  return bridge()['SetUserVolume'](userID, volume)
}

export function GetUserVolume(userID: number): Promise<number> {
  return bridge()['GetUserVolume'](userID)
}

// --- Chat bindings ---

export function SendChat(message: string): Promise<string> {
  return bridge()['SendChat'](message)
}

export function EditMessage(msgID: number, message: string): Promise<string> {
  return bridge()['EditMessage'](msgID, message)
}

export function DeleteMessage(msgID: number): Promise<string> {
  return bridge()['DeleteMessage'](msgID)
}

export function AddReaction(msgID: number, emoji: string): Promise<string> {
  return bridge()['AddReaction'](msgID, emoji)
}

export function RemoveReaction(msgID: number, emoji: string): Promise<string> {
  return bridge()['RemoveReaction'](msgID, emoji)
}

// --- Moderation bindings ---

export function KickUser(id: number): Promise<string> {
  return bridge()['KickUser'](id)
}

export function RenameServer(name: string): Promise<string> {
  return bridge()['RenameServer'](name)
}

export function RenameUser(name: string): Promise<string> {
  return bridge()['RenameUser'](name)
}

// --- Invite link / startup ---

export function GetStartupAddr(): Promise<string> {
  return bridge()['GetStartupAddr']()
}

export function GetBuildInfo(): Promise<{
  commit: string
  build_time: string
  go_version: string
  goos: string
  goarch: string
  dirty: boolean
}> {
  return bridge()['GetBuildInfo']()
}

// --- Channel bindings ---

export function JoinChannel(id: number): Promise<string> {
  return bridge()['JoinChannel'](id)
}

export function ConnectVoice(channelID: number): Promise<string> {
  return bridge()['ConnectVoice'](channelID)
}

export function SendChannelChat(channelID: number, message: string): Promise<string> {
  return bridge()['SendChannelChat'](channelID, message)
}

// --- Channel management bindings (owner-only) ---

export function CreateChannel(name: string): Promise<string> {
  return bridge()['CreateChannel'](name)
}

export function RenameChannel(id: number, name: string): Promise<string> {
  return bridge()['RenameChannel'](id, name)
}

export function DeleteChannel(id: number): Promise<string> {
  return bridge()['DeleteChannel'](id)
}

export function MoveUserToChannel(userID: number, channelID: number): Promise<string> {
  return bridge()['MoveUserToChannel'](userID, channelID)
}

// --- File upload bindings ---

export function UploadFile(channelID: number): Promise<string> {
  return bridge()['UploadFile'](channelID)
}

export function UploadFileFromPath(channelID: number, path: string): Promise<string> {
  return bridge()['UploadFileFromPath'](channelID, path)
}

// --- Video bindings ---

export function StartVideo(): Promise<string> {
  return bridge()['StartVideo']()
}

export function StopVideo(): Promise<string> {
  return bridge()['StopVideo']()
}

export function StartScreenShare(): Promise<string> {
  return bridge()['StartScreenShare']()
}

export function StopScreenShare(): Promise<string> {
  return bridge()['StopScreenShare']()
}

// --- Video quality bindings ---

export function RequestVideoQuality(targetUserID: number, quality: string): Promise<string> {
  return bridge()['RequestVideoQuality'](targetUserID, quality)
}

// --- Pull-based state request bindings ---

export function RequestChannels(): Promise<string> {
  return bridge()['RequestChannels']()
}

export function RequestMessages(channelID: number): Promise<string> {
  return bridge()['RequestMessages'](channelID)
}

export function RequestServerInfo(): Promise<string> {
  return bridge()['RequestServerInfo']()
}
