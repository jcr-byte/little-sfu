package signaling

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestWatchHandlerRejectsInvalidRoomID(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/watch/test.room",
		strings.NewReader(`{"sdp":"offer-sdp","type":"offer"}`),
	)
	request.SetPathValue("room", "test.room")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	NewServer().WatchHandler(response, request)

	assertResponse(t, response, http.StatusBadRequest, "invalid room ID\n")
}

func TestWatchHandlerRejectsUnavailablePublisher(t *testing.T) {
	tests := []struct {
		name       string
		roomExists bool
		hasAudio   bool
		hasVideo   bool
	}{
		{name: "missing room"},
		{name: "no tracks", roomExists: true},
		{name: "audio only", roomExists: true, hasAudio: true},
		{name: "video only", roomExists: true, hasVideo: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer()
			const roomID = "test-room"

			if test.roomExists {
				room, ok := server.reserveRoom(roomID)
				if !ok {
					t.Fatal("failed to reserve test room")
				}

				if test.hasAudio {
					room.audioTrack = newWatchTestTrack(
						t,
						webrtc.RTPCodecCapability{
							MimeType:  webrtc.MimeTypeOpus,
							ClockRate: 48000,
							Channels:  2,
						},
						"audio",
					)
				}

				if test.hasVideo {
					room.videoTrack = newWatchTestTrack(
						t,
						webrtc.RTPCodecCapability{
							MimeType:  webrtc.MimeTypeVP8,
							ClockRate: 90000,
						},
						"video",
					)
				}
			}

			// Reuse the SDP fixture with a receive-only viewer offer.
			_, offer := newValidPublishRequest(t, roomID)
			offer.SDP = strings.ReplaceAll(offer.SDP, "a=sendonly", "a=recvonly")

			body, err := json.Marshal(offer)
			if err != nil {
				t.Fatalf("failed to encode watch offer: %v", err)
			}

			request := httptest.NewRequest(
				http.MethodPost,
				"/watch/"+roomID,
				strings.NewReader(string(body)),
			)
			request.SetPathValue("room", roomID)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			server.WatchHandler(response, request)

			assertResponse(t, response, http.StatusConflict, "publisher is not ready\n")
		})
	}
}

func TestWatchHandlerReturnsAnswerForReadyPublisher(t *testing.T) {
	server := NewServer()
	room, _ := server.reserveRoom("test-room")

	room.audioTrack = newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  2,
	}, "audio")
	room.videoTrack = newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP8,
		ClockRate: 90000,
	}, "video")

	_, offer := newValidPublishRequest(t, "test-room")
	offer.SDP = strings.ReplaceAll(offer.SDP, "a=sendonly", "a=recvonly")

	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/watch/test-room",
		strings.NewReader(string(body)),
	)
	request.SetPathValue("room", "test-room")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.WatchHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body)
	}

	var answer webrtc.SessionDescription
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		t.Fatalf("expected JSON SDP answer: %v", err)
	}
	if answer.Type != webrtc.SDPTypeAnswer {
		t.Errorf("expected answer type, got %s", answer.Type)
	}
	if strings.TrimSpace(answer.SDP) == "" {
		t.Error("expected nonempty answer SDP")
	}
}

func TestWatchHandlerCleansUpViewerAfterInvalidSDP(t *testing.T) {
	server := NewServer()
	room, _ := server.reserveRoom("test-room")
	room.audioTrack = newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  2,
	}, "audio")
	room.videoTrack = newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP8,
		ClockRate: 90000,
	}, "video")

	viewer, err := server.newPeerConnection()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { viewer.Close() })
	created := false
	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		created = true
		return viewer, nil
	}

	// Valid JSON reaches connection setup; invalid SDP fails after registration.
	request := httptest.NewRequest(http.MethodPost, "/watch/test-room",
		strings.NewReader(`{"type":"offer","sdp":"invalid SDP"}`))
	request.SetPathValue("room", "test-room")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.WatchHandler(response, request)

	assertResponse(t, response, http.StatusBadRequest, "invalid SDP offer\n")
	if !created {
		t.Fatal("expected signaling to reach viewer connection creation")
	}
	room.mu.RLock()
	remainingViewers := len(room.viewers)
	room.mu.RUnlock()
	if remainingViewers != 0 {
		t.Errorf("expected no registered viewers after signaling failure, got %d", remainingViewers)
	}
	if viewer.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Error("expected viewer connection to be closed after signaling failure")
	}
}

func TestWatchHandlerTimesOutAndReleasesViewer(t *testing.T) {
	server := NewServer()
	server.gatheringTimeout = 20 * time.Millisecond

	neverComplete := make(chan struct{})
	server.gatheringComplete = func(*webrtc.PeerConnection) <-chan struct{} {
		return neverComplete
	}

	// Make the room ready to accept viewers.
	const roomID = "test-room"
	room, _ := server.reserveRoom(roomID)
	t.Cleanup(func() { server.removePublisher(room) })

	room.audioTrack = newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  2,
	}, "audio")
	room.videoTrack = newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeVP8,
		ClockRate: 90000,
	}, "video")

	// Capture the pending viewer connection for the cleanup assertion.
	viewer, err := server.newPeerConnection()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { viewer.Close() })
	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		return viewer, nil
	}

	// Reuse the SDP fixture as a receive-only viewer offer.
	_, offer := newValidPublishRequest(t, roomID)
	offer.SDP = strings.ReplaceAll(offer.SDP, "a=sendonly", "a=recvonly")
	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/watch/"+roomID,
		strings.NewReader(string(body)),
	)
	request.SetPathValue("room", roomID)
	request.Header.Set("Content-Type", "application/json")

	// No request deadline; cancellation only prevents a hanging test.
	ctx, cancel := context.WithCancel(request.Context())
	defer cancel()
	watchdog := time.AfterFunc(5*time.Second, cancel)
	defer watchdog.Stop()

	request = request.WithContext(ctx)
	response := httptest.NewRecorder()
	server.WatchHandler(response, request)

	if ctx.Err() != nil {
		t.Fatal("handler did not finish before the test safety cancellation")
	}

	assertResponse(t, response, http.StatusGatewayTimeout, "ICE gathering timed out\n")

	room.mu.RLock()
	_, registered := room.viewers[viewer]
	room.mu.RUnlock()
	if registered {
		t.Error("timed-out viewer is still registered")
	}
	if got := viewer.ConnectionState(); got != webrtc.PeerConnectionStateClosed {
		t.Errorf("viewer connection state = %s, want closed", got)
	}
}

func newWatchTestTrack(
	t *testing.T,
	codec webrtc.RTPCodecCapability,
	id string,
) *webrtc.TrackLocalStaticRTP {
	t.Helper()

	track, err := webrtc.NewTrackLocalStaticRTP(codec, id, "test-stream")
	if err != nil {
		t.Fatalf("failed to create test track: %v", err)
	}
	return track
}
