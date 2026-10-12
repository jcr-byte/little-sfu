# Little SFU

A small selective forwarding unit built from scratch with
[Pion WebRTC](https://github.com/pion/webrtc). This project is intended to teach
WebRTC signaling, RTP/RTCP forwarding, media negotiation, and peer lifecycle
management.

Participants join a room to send audio and video and receive media from the other
participants in the browser. The server forwards RTP packets as they
arrive, without transcoding or HLS segmenting.

## Project status

The **v0.2 — multi-party conferencing** acceptance test passed on October 12,
2026: three browser participants could publish, receive media, leave, and rejoin
during a 30-minute session. Automated Go and browser tests also pass.

The next milestone is **v0.3 — network resilience**.

## Run locally

Install Go 1.27 or newer. From the repository root, start the server:

```sh
go run ./cmd/sfu
```

Open <http://localhost:8080/?room=demo> in three browser tabs. Click **Join room**
and allow camera and microphone access in each tab. Each participant should see
and hear the others, with a separate video tile for each remote participant.
Use headphones to avoid audio feedback when testing multiple tabs.

Your local camera preview is mirrored horizontally, like a mirror. This changes
only the preview; other participants receive your video in its normal orientation.

Click **Leave** to close your connections and stop your camera and microphone.
The other tabs should remove your video tile. Click **Join room** again to rejoin
without restarting the server. Leaving while camera permission is pending also
cancels that join attempt, even if permission is granted afterward.

Use another room ID in the URL to start an independent call. Press Ctrl+C in the
terminal to stop the server and close active connections.

Run the automated checks with:

```sh
go test -race ./...
node --test internal/web/*.test.mjs
```

The server loads `internal/web/index.html` using a path relative to its working
directory, so start it from the repository root. Browser camera and microphone
access requires permission; `localhost` is treated as a secure context by browsers.

## Learning objectives

Little SFU explores how to:

1. Negotiate WebRTC connections with Pion.
2. Receive, inspect, and forward RTP packets.
3. Process RTCP feedback such as PLI and NACK.
4. Manage rooms, tracks, and peer lifecycles.
5. Add renegotiation for multi-party calls.
6. Explore simulcast and congestion control.

The project aims to produce a small multi-party SFU whose media paths and connection
lifecycle are easy to understand. It should clean up connections correctly and help
late viewers begin decoding video quickly.

## How it works

Each participant sends media to the SFU, which forwards RTP packets to the other
participants in the same room. A participant does not subscribe to their own
tracks. WebSocket signaling stays open so the server can renegotiate when tracks
are added or removed as participants join and leave.

Later milestones add packet-loss recovery, simulcast, and congestion control.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the detailed design.

## Signaling API

The conferencing browser uses `GET /join/{room}` to open a WebSocket connection.
The server sends JSON SDP offers and the browser returns JSON SDP answers after
ICE gathering completes. The same connection carries later negotiations when
room tracks change. Closing it removes the participant from the room.

Room IDs must contain 1–64 ASCII letters, digits, underscores, or hyphens.

The legacy v0.1 broadcast endpoints remain available:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/publish/{room}` | Negotiate the room's publisher connection |
| `POST` | `/watch/{room}` | Negotiate a receive-only viewer connection |

Both endpoints accept a JSON SDP offer and return a JSON SDP answer after ICE
gathering completes. A caller must first create and gather its own WebRTC offer. A request body has this shape:

```json
{"type":"offer","sdp":"v=0\r\n..."}
```

Use `Content-Type: application/json`. A successful response has status `200`, the
same content type, and a body shaped like
`{"type":"answer","sdp":"v=0\r\n..."}`. Room IDs must contain 1–64 ASCII
letters, digits, underscores, or hyphens. Request bodies are limited to 64 KiB.

Publishing to an occupied room returns `409 Conflict`. Watching before the
publisher sends both audio and video also returns `409 Conflict`; completing
`/publish/{room}` alone does not make the room ready. Error responses are plain
text.

## Current limits

- Rejoining is manual through **Join room**; there is no automatic reconnect.
- The legacy broadcast API supports one publisher per room, sending Opus audio
  and VP8 video, with receive-only viewers.
- No authentication, recording, persistence, or distributed deployment.
- No STUN or TURN server configuration. Localhost is the supported demo setup;
  connectivity across other networks is not established.
- The legacy broadcast path uses periodic PLI to request video keyframes about
  every three seconds. On-demand keyframe requests for conferencing and packet-loss
  recovery are planned for v0.3; late-join video start time is not yet guaranteed.

## Development milestones

| Version | Outcome | Main topics |
|---|---|---|
| v0.1 | One-to-many broadcast | Tracks, RTP forwarding, SDP negotiation |
| v0.2 | Multi-party conferencing | Renegotiation, N-way publishing |
| v0.3 | Network resilience | On-demand PLI, NACK, retransmission |
| v0.4 | Adaptive quality | Simulcast, per-viewer layer selection |
| v0.5 | Congestion control | TWCC, bandwidth estimation, pacing |

See [ROADMAP.md](ROADMAP.md) for target dates, release scope, and acceptance criteria.

## Scope

The project deliberately prioritizes learning Pion and WebRTC internals. The
following are secondary or out of scope:

- Authentication and authorization
- Recording and persistent storage
- Distributed deployment and production scaling
- A polished conferencing interface

Simulcast and congestion control are optional advanced exercises. Completing v0.3
with a clear understanding of its implementation is already a successful learning
outcome.

## Documentation

- [Architecture](ARCHITECTURE.md) — design, media flow, lifecycle, and build order
- [Roadmap](ROADMAP.md) — dates, release scope, and acceptance criteria

## License

MIT
