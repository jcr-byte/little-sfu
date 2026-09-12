package signaling

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

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
					room.audioTrack = newWatchTestTrack(t,
						webrtc.RTPCodecCapability{
							MimeType:  webrtc.MimeTypeOpus,
							ClockRate: 48000,
							Channels:  2,
						},
						"audio",
					)
				}

				if test.hasVideo {
					room.videoTrack = newWatchTestTrack(t,
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
