# Roadmap

This schedule assumes one developer working roughly 18–20 hours per week, starting
August 24, 2026. Dates are targets, not promises. Stabilization is included in each
release window; if work slips, reduce scope before moving the date.

## Release schedule

| Target date | Milestone | Outcome |
|---|---|---|
| Aug 30, 2026 | Project skeleton | A Go server builds, runs, and serves the browser demo. |
| Sep 6, 2026 | Publisher connection | A browser can publish audio and video; the server logs both incoming tracks. |
| Sep 13, 2026 | First end-to-end broadcast | One viewer can receive a publisher's audio and video through the SFU. |
| Sep 20, 2026 | **v0.1.0: broadcast** | Multi-viewer rooms, lifecycle cleanup, tests, and documentation are complete. |
| Oct 11, 2026 | **v0.2.0: conferencing** | Multiple participants can publish and subscribe in one room. |
| Nov 1, 2026 | **v0.3.0: network resilience** | Late joins recover quickly and common packet loss is handled. |
| Nov 22, 2026 | **v0.4.0: adaptive quality** | Simulcast publishers and per-viewer layer selection work. |
| Dec 13, 2026 | **v0.5.0: congestion control** | The server adapts and paces traffic under constrained bandwidth. |

## v0.1 — Broadcast

Target: September 20, 2026

Scope:

- One publisher and many receive-only viewers per room
- Audio and video forwarding using Opus and VP8
- Multiple independent rooms
- Non-trickle HTTP signaling
- Browser demo for publishing and watching
- Periodic PLI as a temporary late-join workaround
- Graceful shutdown and peer cleanup

Done when:

- Two viewers can watch the same publisher for 30 minutes without interruption.
- Starting a second room does not affect the first.
- A publisher or viewer can disconnect and reconnect without restarting the server.
- Publisher disconnect removes its room and viewers without leaked goroutines.
- Unit tests cover room registration and lifecycle; an integration test covers SDP
  negotiation and packet forwarding.
- Setup, API behavior, limitations, and a manual smoke test are documented.

Not included: renegotiation, multiple publishers, NACK/RTX, simulcast, bandwidth
estimation, authentication, persistence, or recording.

## v0.2 — Conferencing

Target: October 11, 2026

Scope:

- Multiple publishers in a room
- Track add/remove renegotiation
- Participants joining and leaving without tearing down the room
- Reconnection behavior

Done when three browser participants can publish audio and video, see one another,
and independently join, leave, and reconnect during a 30-minute test.

## v0.3 — Network resilience

Target: November 1, 2026

Scope:

- On-demand PLI for late joiners
- RTP sequence tracking
- NACK handling and retransmission support
- Basic packet-loss metrics

Done when a late viewer gets decodable video within two seconds and a controlled
loss test shows recovery from moderate packet loss without restarting a session.

## v0.4 — Adaptive quality

Target: November 22, 2026

Scope:

- Simulcast ingestion
- A separate outgoing track and selected layer per viewer
- Layer switching based on configured viewer constraints
- Quality-selection metrics

Done when viewers can independently receive low, medium, or high video layers and
switch layers without reconnecting.

## v0.5 — Congestion control

Target: December 13, 2026

Scope:

- Transport-wide congestion control feedback
- Per-viewer bandwidth estimation
- Packet pacing
- Automatic simulcast layer choice

Done when a throttled viewer automatically moves to a sustainable layer without
degrading other viewers in the room.

## Review rhythm

- Every Sunday: record what shipped, what is blocked, and the next week's single
  most important deliverable.
- At each release: run the acceptance test, tag the release, and write brief release
  notes before beginning the next version.
- After two consecutive missed weekly targets: revise the remaining dates or cut
  scope explicitly instead of silently carrying unfinished work forward.
