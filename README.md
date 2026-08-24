# Little SFU

A small selective forwarding unit built from scratch with
[Pion WebRTC](https://github.com/pion/webrtc). This project is intended to teach
WebRTC signaling, RTP/RTCP forwarding, media negotiation, and peer lifecycle
management—not to become a production conferencing platform.

The first release will let one publisher send audio and video into a room while
multiple viewers watch in the browser with sub-second latency. The server will
forward RTP packets as they arrive, without transcoding or HLS segmenting.

## Project status

Current milestone: **v0.1 — one-to-many broadcast**

The repository is currently in the planning stage. The architecture and roadmap are
documented, but the server has not been implemented yet.

- [ ] Create the Go server and browser demo
- [ ] Accept a publisher connection
- [ ] Forward audio and video to one viewer
- [ ] Support multiple viewers and independent rooms
- [ ] Clean up disconnected peers and rooms
- [ ] Add a temporary periodic PLI for late viewers
- [ ] Test, document, and tag v0.1.0

The target date for v0.1.0 is **October 4, 2026**.

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

## Planned interface

The planned v0.1 signaling API is:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/publish/{room}` | Negotiate the room's publisher connection |
| `POST` | `/watch/{room}` | Negotiate a receive-only viewer connection |

Both endpoints will accept an SDP offer and return an SDP answer after ICE gathering
completes. Installation and usage instructions will be added when the first runnable
release is available.

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
