---
layout: home

hero:
  name: BKEN
  text: Voice chat you own.
  tagline: Self-hosted, end-to-end encrypted, and light enough to run on the cheapest server you can find. No accounts. No cloud. Just talk.
  actions:
    - theme: brand
      text: Download
      link: /download
    - theme: alt
      text: View on GitHub
      link: https://github.com/rustyguts/bken

features:
  - icon: 🔒
    title: Encrypted by default
    details: Signaling is TLS 1.3 over WebSocket; audio is encrypted DTLS-SRTP via WebRTC — the same standard used by every browser call. There is no "turn on encryption" toggle because it was never off.

  - icon: ⚡
    title: Sub-50ms latency
    details: Peer-to-peer WebRTC with Opus audio at 48 kHz. Conversations feel like phone calls, not video conferences. Silence is transmitted as nothing, not silence.

  - icon: 💻
    title: Cross-platform client
    details: Native desktop apps for macOS, Linux, and Windows. Lightweight and self-contained — no Electron, no browser required.

  - icon: 🏠
    title: Run it for $4 a month
    details: The relay server is a single Go binary. A 1-core, 1 GB droplet can comfortably serve a channel of 100 people. Your voice never touches infrastructure you don't control.

  - icon: 🎙️
    title: Noise suppression built in
    details: ML-based cancellation powered by RNNoise removes keyboard clicks, fans, and background noise before audio is transmitted — not after.

  - icon: 🚀
    title: Zero configuration
    details: Run the server, open the client, type a name and an address. That is the entire setup. No DNS, no certificates to manage, no firewall rules beyond one TCP port.
---
