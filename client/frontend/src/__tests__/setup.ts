/**
 * Test setup: mocks for the Wails runtime and Go bridge bindings.
 *
 * The real Wails runtime uses window.runtime and window.go which don't exist
 * in jsdom. We create a lightweight event bus and stub every Go function so
 * components can be mounted without errors.
 */
import { vi } from 'vitest'

// ---------------------------------------------------------------------------
// localStorage mock (jsdom doesn't always provide a full localStorage)
// ---------------------------------------------------------------------------

const store: Record<string, string> = {}
if (!globalThis.localStorage || typeof globalThis.localStorage.setItem !== 'function') {
  Object.defineProperty(globalThis, 'localStorage', {
    value: {
      getItem: (key: string) => store[key] ?? null,
      setItem: (key: string, value: string) => { store[key] = value },
      removeItem: (key: string) => { delete store[key] },
      clear: () => { for (const k of Object.keys(store)) delete store[k] },
      get length() { return Object.keys(store).length },
      key: (index: number) => Object.keys(store)[index] ?? null,
    },
    writable: true,
    configurable: true,
  })
}

// ---------------------------------------------------------------------------
// Wails runtime event bus mock
// ---------------------------------------------------------------------------

type Listener = { cb: (...args: any[]) => void; remaining: number }

const listeners = new Map<string, Listener[]>()

function eventsOn(name: string, cb: (...args: any[]) => void, max = -1) {
  if (!listeners.has(name)) listeners.set(name, [])
  listeners.get(name)!.push({ cb, remaining: max })
  return () => eventsOff(name)
}

function eventsOff(...names: string[]) {
  for (const n of names) listeners.delete(n)
}

function eventsOffAll() {
  listeners.clear()
}

function eventsEmit(name: string, ...data: any[]) {
  const list = listeners.get(name)
  if (!list) return
  const keep: Listener[] = []
  for (const l of list) {
    l.cb(...data)
    if (l.remaining > 0) l.remaining--
    if (l.remaining !== 0) keep.push(l)
  }
  listeners.set(name, keep)
}

/** Fire a Wails-style event in tests so components receive it. */
export function emitWailsEvent(name: string, ...data: any[]) {
  eventsEmit(name, ...data)
}

/** Reset the event bus between tests. */
export function resetWailsEvents() {
  listeners.clear()
}

// Assign to window.runtime so the wailsjs/runtime/runtime.js module works.
;(window as any).runtime = {
  EventsOnMultiple: eventsOn,
  EventsOn: (n: string, cb: (...args: any[]) => void) => eventsOn(n, cb, -1),
  EventsOnce: (n: string, cb: (...args: any[]) => void) => eventsOn(n, cb, 1),
  EventsOff: eventsOff,
  EventsOffAll: eventsOffAll,
  EventsEmit: eventsEmit,
  LogPrint: vi.fn(),
  LogTrace: vi.fn(),
  LogDebug: vi.fn(),
  LogInfo: vi.fn(),
  LogWarning: vi.fn(),
  LogError: vi.fn(),
  LogFatal: vi.fn(),
  WindowReload: vi.fn(),
  WindowReloadApp: vi.fn(),
  WindowSetAlwaysOnTop: vi.fn(),
  WindowSetSystemDefaultTheme: vi.fn(),
  WindowSetLightTheme: vi.fn(),
  WindowSetDarkTheme: vi.fn(),
  WindowCenter: vi.fn(),
  WindowSetTitle: vi.fn(),
  WindowFullscreen: vi.fn(),
  WindowUnfullscreen: vi.fn(),
  WindowIsFullscreen: vi.fn().mockResolvedValue(false),
  WindowGetSize: vi.fn().mockResolvedValue({ w: 1280, h: 720 }),
  WindowSetSize: vi.fn(),
  WindowSetMaxSize: vi.fn(),
  WindowSetMinSize: vi.fn(),
  WindowSetPosition: vi.fn(),
  WindowGetPosition: vi.fn().mockResolvedValue({ x: 0, y: 0 }),
  WindowHide: vi.fn(),
  WindowShow: vi.fn(),
  WindowMaximise: vi.fn(),
  WindowToggleMaximise: vi.fn(),
  WindowUnmaximise: vi.fn(),
  WindowIsMaximised: vi.fn().mockResolvedValue(false),
  WindowMinimise: vi.fn(),
  WindowUnminimise: vi.fn(),
  WindowIsMinimised: vi.fn().mockResolvedValue(false),
  WindowIsNormal: vi.fn().mockResolvedValue(true),
  WindowSetBackgroundColour: vi.fn(),
  ScreenGetAll: vi.fn().mockResolvedValue([]),
  BrowserOpenURL: vi.fn(),
  Environment: vi.fn().mockResolvedValue({ buildType: 'test', platform: 'linux', arch: 'amd64' }),
  Quit: vi.fn(),
  Hide: vi.fn(),
  Show: vi.fn(),
  ClipboardGetText: vi.fn().mockResolvedValue(''),
  ClipboardSetText: vi.fn().mockResolvedValue(true),
  OnFileDrop: vi.fn(),
  OnFileDropOff: vi.fn(),
  CanResolveFilePaths: vi.fn().mockReturnValue(false),
  ResolveFilePaths: vi.fn(),
}

// ---------------------------------------------------------------------------
// Go bridge mock  (window.go.main.App)
// ---------------------------------------------------------------------------

const defaultConfig = {
  theme: 'dark',
  theme_mode: 'manual',
  username: 'TestUser',
  input_device_id: 0,
  output_device_id: 0,
  volume: 1,
  audio_bitrate_kbps: 32,
  noise_enabled: false,
  noise_level: 0,
  aec_enabled: false,
  agc_enabled: false,
  agc_level: 0,
  ptt_enabled: false,
  ptt_key: 'Backquote',
  servers: [{ name: 'Local Dev', addr: 'localhost:8080' }],
  message_density: 'default',
  show_system_messages: true,
}

let savedConfig = { ...defaultConfig }

export function resetConfig() {
  savedConfig = { ...defaultConfig }
}

const goApp: Record<string, any> = {
  Connect: vi.fn().mockResolvedValue(''),
  Disconnect: vi.fn().mockResolvedValue(undefined),
  DisconnectVoice: vi.fn().mockResolvedValue(''),
  ConnectVoice: vi.fn().mockResolvedValue(''),
  GetAutoLogin: vi.fn().mockResolvedValue({ username: '', addr: '' }),
  GetConfig: vi.fn().mockImplementation(() => Promise.resolve({ ...savedConfig })),
  SaveConfig: vi.fn().mockImplementation((cfg: any) => { savedConfig = { ...cfg }; return Promise.resolve() }),
  ApplyConfig: vi.fn().mockResolvedValue(undefined),
  GetStartupAddr: vi.fn().mockResolvedValue(''),
  SendChat: vi.fn().mockResolvedValue(''),
  SendChannelChat: vi.fn().mockResolvedValue(''),
  EditMessage: vi.fn().mockResolvedValue(''),
  DeleteMessage: vi.fn().mockResolvedValue(''),
  AddReaction: vi.fn().mockResolvedValue(''),
  RemoveReaction: vi.fn().mockResolvedValue(''),
  SendTyping: vi.fn().mockResolvedValue(''),
  JoinChannel: vi.fn().mockResolvedValue(''),
  CreateChannel: vi.fn().mockResolvedValue(''),
  RenameChannel: vi.fn().mockResolvedValue(''),
  DeleteChannel: vi.fn().mockResolvedValue(''),
  MoveUserToChannel: vi.fn().mockResolvedValue(''),
  KickUser: vi.fn().mockResolvedValue(''),
  UploadFile: vi.fn().mockResolvedValue(''),
  UploadFileFromPath: vi.fn().mockResolvedValue(''),
  RenameUser: vi.fn().mockResolvedValue(''),
  RenameServer: vi.fn().mockResolvedValue(''),
  SetMuted: vi.fn().mockResolvedValue(undefined),
  SetDeafened: vi.fn().mockResolvedValue(undefined),
  SetAEC: vi.fn().mockResolvedValue(undefined),
  SetAGC: vi.fn().mockResolvedValue(undefined),
  SetAGCLevel: vi.fn().mockResolvedValue(undefined),
  SetAudioBitrate: vi.fn().mockResolvedValue(undefined),
  GetAudioBitrate: vi.fn().mockResolvedValue(32),
  GetBuildInfo: vi.fn().mockResolvedValue({
    commit: 'deadbeefcaf0',
    build_time: '2026-02-21T00:00:00Z',
    go_version: 'go1.25.0',
    goos: 'darwin',
    goarch: 'arm64',
    dirty: false,
  }),
  SetNoiseSuppression: vi.fn().mockResolvedValue(undefined),
  SetNoiseSuppressionLevel: vi.fn().mockResolvedValue(undefined),
  SetNotificationVolume: vi.fn().mockResolvedValue(undefined),
  GetNotificationVolume: vi.fn().mockResolvedValue(0.5),
  SetPTTMode: vi.fn().mockResolvedValue(undefined),
  PTTKeyDown: vi.fn().mockResolvedValue(undefined),
  PTTKeyUp: vi.fn().mockResolvedValue(undefined),
  MuteUser: vi.fn().mockResolvedValue(undefined),
  UnmuteUser: vi.fn().mockResolvedValue(undefined),
  GetMutedUsers: vi.fn().mockResolvedValue([]),
  SetUserVolume: vi.fn().mockResolvedValue(undefined),
  GetUserVolume: vi.fn().mockResolvedValue(1.0),
  GetInputDevices: vi.fn().mockResolvedValue([]),
  GetOutputDevices: vi.fn().mockResolvedValue([]),
  GetInputLevel: vi.fn().mockResolvedValue(0),
  GetMetrics: vi.fn().mockResolvedValue({ latency: 0, jitter: 0, loss: 0 }),
  SetInputDevice: vi.fn().mockResolvedValue(undefined),
  SetOutputDevice: vi.fn().mockResolvedValue(undefined),
  SetVolume: vi.fn().mockResolvedValue(undefined),
  StartTest: vi.fn().mockResolvedValue(''),
  StopTest: vi.fn().mockResolvedValue(undefined),
  IsConnected: vi.fn().mockResolvedValue(false),
  StartVideo: vi.fn().mockResolvedValue(''),
  StopVideo: vi.fn().mockResolvedValue(''),
  StartScreenShare: vi.fn().mockResolvedValue(''),
  StopScreenShare: vi.fn().mockResolvedValue(''),
  StartRecording: vi.fn().mockResolvedValue(''),
  StopRecording: vi.fn().mockResolvedValue(''),
  RequestVideoQuality: vi.fn().mockResolvedValue(''),
  RequestChannels: vi.fn().mockResolvedValue(''),
  RequestMessages: vi.fn().mockResolvedValue(''),
  RequestServerInfo: vi.fn().mockResolvedValue(''),
}

;(window as any).go = {
  main: {
    App: goApp,
  },
}

/** Get the Go bridge mock to assert calls in tests. */
export function getGoMock() {
  return goApp
}

/** Utility to flush all pending promises. */
export function flushPromises() {
  return new Promise<void>(resolve => setTimeout(resolve, 0))
}

// Suppress unhandled rejection errors from template ref focus() calls in jsdom.
// In jsdom, template refs inside v-if conditionals may not resolve to proper
// HTMLElements, causing .focus() to fail. This is harmless in tests.
if (typeof process !== 'undefined') {
  process.on('unhandledRejection', (err: any) => {
    if (err?.message?.includes('focus is not a function')) return
    throw err
  })
}

// Auto-reset event bus between tests.
import { afterEach } from 'vitest'

afterEach(() => {
  resetWailsEvents()
  resetConfig()
  // Reset all Go mocks
  for (const fn of Object.values(goApp)) {
    if (typeof fn === 'function' && 'mockClear' in fn) {
      fn.mockClear()
    }
  }
})
