# Architecture

This document describes the design of v0.1 and explains the Pion concepts behind
it. The goal is to make each decision understandable while leaving room for later
experiments.

## What v0.1 does

Each room contains one publisher and any number of viewers. The publisher sends
audio and video to the server. The server forwards those packets to every viewer:

```text
Publisher ──WebRTC──> Pion SFU ──WebRTC──> Viewer A
                              ├───────────> Viewer B
                              └───────────> Viewer C
```

The server does not decode, mix, or transcode media. It receives RTP packets and
forwards them using Pion.

## The central forwarding loop

Pion represents a track received from the publisher as a `*webrtc.TrackRemote`.
Viewers need a local track that Pion can send, represented by
`*webrtc.TrackLocalStaticRTP`.

The central operation is therefore:

```text
TrackRemote.ReadRTP() ──> TrackLocalStaticRTP.WriteRTP()
```

One local track can be attached to multiple viewer `PeerConnection`s. Writing one
packet to that track lets Pion send the packet to every attached viewer.

```go
func forward(remote *webrtc.TrackRemote, local *webrtc.TrackLocalStaticRTP) {
    for {
        packet, _, err := remote.ReadRTP()
        if err != nil {
            return
        }

        if err := local.WriteRTP(packet); err != nil {
            // A shared local track writes to several viewers. One viewer may fail
            // while the others remain healthy, so do not stop forwarding here.
            log.Printf("RTP write failed for one or more viewers: %v", err)
        }
    }
}
```

The typed RTP methods are intentional. They make packet headers visible while
learning, and `TrackLocalStaticRTP` must inspect the packet when adapting it to each
viewer anyway. Optimization can wait until profiling shows a real problem.

## Main types

```go
// Server owns the room registry. There is one Server per process.
type Server struct {
    mu    sync.RWMutex
    rooms map[string]*Room
}

// Room owns one broadcast and its membership.
type Room struct {
    ID string

    mu        sync.RWMutex
    publisher *Publisher
    viewers   map[string]*Viewer
}

type PublisherState int

const (
    PublisherConnecting PublisherState = iota
    PublisherReady
    PublisherClosed
)

type Publisher struct {
    pc      *webrtc.PeerConnection
    state   PublisherState
    sources []Source
    once    sync.Once
}

type Viewer struct {
    ID      string
    pc      *webrtc.PeerConnection
    release []func()
    once    sync.Once
}
```

The publisher state matters because completing SDP negotiation does not mean media
has arrived. Pion calls `OnTrack` only when incoming media begins. Audio and video
may also arrive at different times.

For v0.1, a publisher becomes `PublisherReady` after the expected audio and video
tracks have arrived. A watch request made before then receives `409 Conflict` with a
clear "publisher is not ready" response. This prevents a viewer from negotiating a
connection with missing tracks that v0.1 cannot add later.

## Ownership and locking

The ownership rules are:

- `Server` owns the room map.
- A `Room` owns its publisher and viewers.
- A `Publisher` or `Viewer` owns its `PeerConnection`.
- A `Viewer` owns the cleanup functions returned by its subscriptions.

Use `Server.mu` only to access the room map. Use `Room.mu` for room membership,
publisher state, and the publisher's source list.

Never hold both locks at once. Never hold a lock while performing SDP operations,
subscribing, writing RTP, or closing a `PeerConnection`; those operations may block
or trigger callbacks.

When a handler needs the source list, copy the slice while holding `Room.mu`, then
release the lock before using it:

```go
room.mu.RLock()
sources := append([]Source(nil), room.publisher.sources...)
room.mu.RUnlock()
```

Connection callbacks may run more than once or race with an HTTP handler. Cleanup
must therefore be idempotent. Each peer uses `sync.Once` so its resources are
released exactly once.

## Sources and subscriptions

A `Source` represents one incoming media stream from the point of view of a viewer:

```go
type Source interface {
    Subscribe(viewerID string) (webrtc.TrackLocal, func(), error)
    Kind() webrtc.RTPCodecType
}
```

The cleanup function lets a later implementation allocate per-viewer resources
without changing the viewer lifecycle code.

In v0.1, all viewers share one local track:

```go
type singleLayer struct {
    local *webrtc.TrackLocalStaticRTP
}

func (s *singleLayer) Subscribe(string) (webrtc.TrackLocal, func(), error) {
    return s.local, func() {}, nil
}
```

This is a useful starting boundary, not a promise that simulcast will require no
other changes. Switching simulcast layers later may require waiting for keyframes
and rewriting sequence numbers and timestamps so the outgoing stream stays
continuous. Design that behavior when v0.4 begins instead of building it now.

## Signaling

v0.1 uses two HTTP endpoints:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/publish/{room}` | Create the publisher connection |
| `POST` | `/watch/{room}` | Create a receive-only viewer connection |

Requests contain an SDP offer and successful responses contain an SDP answer. ICE
gathering completes before the response is sent, so the browser and server exchange
only one signaling request. This is called non-trickle ICE.

The handlers must:

- Accept only `POST` requests with the expected content type.
- Limit the request-body size.
- Validate room IDs and impose a reasonable length limit.
- Set a timeout for ICE gathering.
- Stop waiting if the HTTP request is cancelled.
- Close and unregister provisional peer connections after an error or timeout.

Non-trickle ICE is slower than trickle ICE, but it keeps the first signaling system
small enough to understand. WebSocket signaling can be introduced with
renegotiation in v0.2.

## Publisher request flow

`POST /publish/{room}` performs these steps:

1. Validate the room ID and SDP offer.
2. Reserve the room with a publisher in `PublisherConnecting` state. Return
   `409 Conflict` if the room already has a publisher.
3. Create a `PeerConnection` configured to receive VP8 video and Opus audio.
4. Add receive-only audio and video transceivers.
5. Register `OnTrack` and connection-state callbacks.
6. Apply the remote offer and create and apply the answer.
7. Wait for ICE gathering, request cancellation, or a timeout.
8. Return the complete local SDP answer.

For each `OnTrack` callback:

1. Create a `TrackLocalStaticRTP` with the remote track's codec, ID, and stream ID.
2. Wrap it in a `singleLayer` source.
3. Add the source under `Room.mu`.
4. Mark the publisher ready after its expected tracks have arrived.
5. Start one forwarding goroutine for the track.

For the first experiment, audio and video are both expected. Supporting optional
tracks can be added after the basic flow works.

## Viewer request flow

`POST /watch/{room}` performs these steps:

1. Find the room and confirm its publisher is `PublisherReady`. Otherwise return
   `409 Conflict`.
2. Copy the source list while holding `Room.mu`, then release the lock.
3. Create the server side of the viewer `PeerConnection`. The browser is
   receive-only; the server sends the subscribed tracks.
4. Subscribe to each source and add its returned track to the connection.
5. Start draining RTCP from every `RTPSender` returned by `AddTrack`.
6. Save every subscription cleanup function on the viewer.
7. Register connection-state callbacks.
8. Apply the remote offer and create and apply the answer.
9. Wait for ICE gathering, cancellation, or timeout.
10. Add the completed viewer to the room and return its SDP answer.

If any step fails, close the peer connection and run all subscription cleanup
functions before returning the error.

## RTCP and keyframes

RTP carries the audio and video. RTCP carries feedback and statistics about those
streams.

Every `RTPSender` returned by `AddTrack` has incoming RTCP that must be read. This
allows Pion's interceptors to process feedback and prevents unread data from
accumulating:

```go
func drainRTCP(sender *webrtc.RTPSender) {
    buf := make([]byte, 1500)
    for {
        if _, _, err := sender.Read(buf); err != nil {
            return
        }
    }
}
```

A viewer joining between video keyframes cannot decode immediately. In v0.1, the
publisher connection uses Pion's interval-PLI interceptor to request a keyframe
approximately every three seconds. In v0.3, viewer feedback will be studied and a
keyframe will be requested when a viewer subscribes.

Pion provides default NACK interceptors. To make the learning milestones explicit,
this project will configure its `MediaEngine` and interceptor registry directly:

- v0.1 enables the basic reports needed for a valid connection and interval PLI.
- v0.3 deliberately enables and examines NACK generation and response behavior.
- Custom retransmission code is not required unless it becomes a separate learning
  experiment.

This avoids claiming that packet-loss recovery is absent when Pion may have enabled
it automatically through default configuration.

## Connection lifecycle

Register `OnConnectionStateChange` on every peer connection.

- `Failed` or `Closed`: clean up the peer immediately.
- `Disconnected`: start a short grace timer rather than immediately deleting it.
- `Connected`: cancel a pending disconnect timer.
- Still disconnected when the grace period expires: clean up the peer.

Temporary disconnections can recover, so `Disconnected` is not immediately treated
as permanent failure.

When the v0.1 publisher is removed:

1. Mark it closed and close its connection.
2. Remove the room from the server only if it still contains that publisher.
3. Close every viewer and run its subscription cleanup functions.

Closing a peer connection causes blocked media reads to return, allowing forwarding
and RTCP goroutines to stop naturally. `sync.Once` prevents duplicate cleanup when
several callbacks observe the same failure.

## Room rules

v0.1 uses these predictable behaviors:

- Publishing to an occupied room returns `409 Conflict`.
- Watching a missing or not-yet-ready publisher returns `409 Conflict`.
- A room ID can be reused after its publisher has been fully removed.
- Viewer IDs are generated by the server.
- Invalid or excessively long room IDs return `400 Bad Request`.

## Project layout

```text
cmd/sfu/main.go          Flags, server wiring, and graceful shutdown
internal/room/room.go    Rooms, publishers, viewers, and lifecycle
internal/room/source.go  Source interface and single-layer forwarding
internal/signal/http.go  Publish and watch HTTP handlers
internal/web/index.html  Minimal browser publishing and viewing demo
```

## Build order

Each step should leave the project runnable and teach one new concept:

1. **Server skeleton:** start an HTTP server and serve the browser page.
2. **Publisher negotiation:** accept an offer, return an answer, and log connection
   state changes.
3. **Incoming media:** add `OnTrack` and log the audio/video codec and RTP packet
   count. Do not add viewers yet.
4. **One viewer:** create local tracks and forward media to one viewer.
5. **Multiple viewers:** verify that one shared local track feeds several viewer
   connections.
6. **Rooms and readiness:** isolate rooms and reject viewers until tracks are ready.
7. **Lifecycle:** test disconnects, timeouts, cleanup, and room reuse.
8. **Late viewers:** add interval PLI and verify that a late viewer gets video within
   a few seconds.
9. **Verification:** run unit tests, an end-to-end browser test, and
   `go test -race ./...`.

Tag v0.1.0 only after all nine steps work and the README describes the commands that
actually run.

## Deferred decisions

Do not solve these during v0.1:

- WebSocket and trickle-ICE signaling
- Multi-party renegotiation
- Publisher reconnection without destroying viewer connections
- NACK and retransmission experiments
- Simulcast layer switching and packet continuity
- Bandwidth estimation and pacing
- Authentication, recording, persistence, or distributed scaling

These topics have later roadmap milestones. Keeping them out of v0.1 makes it
possible to understand the basic Pion media path before adding more protocol state.
