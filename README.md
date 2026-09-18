# Little SFU

A small selective forwarding unit built from scratch with
[Pion WebRTC](https://github.com/pion/webrtc). This project is intended to teach
WebRTC signaling, RTP/RTCP forwarding, media negotiation, and peer lifecycle
management.

The first release lets one publisher send audio and video into a room while
multiple viewers watch in the browser. The server forwards RTP packets as they
arrive, without transcoding or HLS segmenting.

## Project status

Current milestone: **v0.1 — one-to-many broadcast**. The Go server, browser demo,
RTP forwarding, room cleanup, periodic keyframe requests, and automated tests are
implemented. The 30-minute viewing and goroutine-leak checks remain before
tagging `v0.1.0`.

The target date for v0.1.0 is **September 20, 2026**.

## Run locally

Install Go 1.27 or newer. From the repository root, start the server:

```sh
go run ./cmd/sfu
```

Open <http://localhost:8080/?room=demo> in a browser. In one tab, allow camera
and microphone access and click **Publish**. After publishing starts, open the same
URL in another tab and click **Watch**. The viewer should receive both audio and
video. Use another room ID in the URL to start an independent broadcast. Press
Ctrl+C in the terminal to stop the server and close active connections.

Run the automated checks with:

```sh
go test -race ./...
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

A publisher sends media to the SFU. For each incoming media track, the SFU forwards
RTP packets to the tracks subscribed to by viewers:

```text
Publisher ──WebRTC──> Pion SFU ──WebRTC──> Viewer A
                              ├───────────> Viewer B
                              └───────────> Viewer C
```

The initial version uses one shared outgoing track for all viewers. Later milestones
add multi-party publishing, packet-loss recovery, simulcast, and congestion control.
See [ARCHITECTURE.md](ARCHITECTURE.md) for the detailed design.

## Signaling API

The v0.1 signaling API is:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/publish/{room}` | Negotiate the room's publisher connection |
| `POST` | `/watch/{room}` | Negotiate a receive-only viewer connection |

Both endpoints accept a JSON SDP offer and return a JSON SDP answer after ICE
gathering completes. The browser demo performs this exchange; a caller must first
create and gather its own WebRTC offer. A request body has this shape:

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

- One publisher per room, sending Opus audio and VP8 video; viewers only receive.
- No renegotiation. If the publisher stops, viewers must reconnect after a new
  publisher starts in that room.
- No authentication, recording, persistence, or distributed deployment.
- No STUN or TURN server configuration. Localhost is the supported demo setup;
  connectivity across other networks is not established.
- A periodic PLI asks the publisher for a video keyframe about every three
  seconds. This is a temporary late-join workaround, not a measured start-time
  guarantee.

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
