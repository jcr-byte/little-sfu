package signaling

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestPublishHandlerRejectsNonPOSTMethod(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/publish/test-room", nil)
	response := httptest.NewRecorder()

	NewServer().PublishHandler(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status %d, got %d", http.StatusMethodNotAllowed, response.Code)
	}

	if got := response.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("expected Allow header %q, got %q", http.MethodPost, got)
	}
}

func TestPublishHandlerRejectsInvalidRoomID(t *testing.T) {
	tests := []struct {
		name   string
		roomID string
	}{
		{name: "missing", roomID: ""},
		{name: "too long", roomID: strings.Repeat("a", 65)},
		{name: "contains a space", roomID: "test room"},
		{name: "contains punctuation", roomID: "test.room"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newPublishRequest(test.roomID, `{"sdp":"offer-sdp","type":"offer"}`)
			response := httptest.NewRecorder()

			NewServer().PublishHandler(response, request)

			assertResponse(t, response, http.StatusBadRequest, "invalid room ID\n")
		})
	}
}

func TestPublishHandlerAcceptsValidRoomIDs(t *testing.T) {
	roomIDs := []string{
		"room",
		"Room-123_test",
		strings.Repeat("a", 64),
	}

	for _, roomID := range roomIDs {
		t.Run(roomID, func(t *testing.T) {
			request, _ := newValidPublishRequest(t, roomID)
			response := httptest.NewRecorder()

			NewServer().PublishHandler(response, request)

			assertResponse(t, response, http.StatusOK, "")
		})
	}
}

func TestPublishHandlerRejectsInvalidContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{name: "missing"},
		{name: "plain text", contentType: "text/plain"},
		{name: "malformed", contentType: "not a content type;"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/publish/test-room", strings.NewReader(`{"sdp":"offer-sdp","type":"offer"}`))
			request.SetPathValue("room", "test-room")
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()

			NewServer().PublishHandler(response, request)

			assertResponse(t, response, http.StatusUnsupportedMediaType, "Content-Type must be application/json\n")
		})
	}
}

func TestPublishHandlerAcceptsJSONContentTypeParameters(t *testing.T) {
	request, _ := newValidPublishRequest(t, "test-room")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response := httptest.NewRecorder()

	NewServer().PublishHandler(response, request)

	assertResponse(t, response, http.StatusOK, "")
}

func TestPublishHandlerRejectsInvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty body", body: "", want: "invalid JSON\n"},
		{name: "malformed", body: `{`, want: "invalid JSON\n"},
		{name: "multiple values", body: `{"sdp":"offer-sdp","type":"offer"} {}`, want: "request body must contain exactly one JSON object\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newPublishRequest("test-room", test.body)
			response := httptest.NewRecorder()

			NewServer().PublishHandler(response, request)

			assertResponse(t, response, http.StatusBadRequest, test.want)
		})
	}
}

func TestPublishHandlerRejectsOversizedBody(t *testing.T) {
	request := newPublishRequest("test-room", strings.Repeat(" ", 64*1024+1))
	response := httptest.NewRecorder()

	NewServer().PublishHandler(response, request)

	assertResponse(t, response, http.StatusRequestEntityTooLarge, "request body too large\n")
}

func TestPublishHandlerRejectsInvalidOffer(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "missing SDP", body: `{"type":"offer"}`, want: "SDP must not be empty\n"},
		{name: "blank SDP", body: `{"sdp":"  \n\t","type":"offer"}`, want: "SDP must not be empty\n"},
		{name: "missing type", body: `{"sdp":"offer-sdp"}`, want: "type must be \"offer\"\n"},
		{name: "wrong type", body: `{"sdp":"offer-sdp","type":"answer"}`, want: "type must be \"offer\"\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := newPublishRequest("test-room", test.body)
			response := httptest.NewRecorder()

			NewServer().PublishHandler(response, request)

			assertResponse(t, response, http.StatusBadRequest, test.want)
		})
	}
}

func TestPublishHandlerRejectsOccupiedRoom(t *testing.T) {
	server := NewServer()
	original, reserved := server.reserveRoom("test-room")
	if !reserved {
		t.Fatal("expected initial room reservation to succeed")
	}

	request, _ := newValidPublishRequest(t, "test-room")
	response := httptest.NewRecorder()

	server.PublishHandler(response, request)

	assertResponse(t, response, http.StatusConflict, "room already has a publisher\n")

	found, ok := server.findRoom("test-room")
	if !ok {
		t.Fatal("expected original room to remain registered")
	}

	if found != original {
		t.Error("expected conflict to preserve the original room")
	}
}

func TestPublishHandlerRemovesReservedRoomWhenPeerConnectionCreationFails(t *testing.T) {
	server := NewServer()
	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		return nil, errors.New("peer connection creation failed")
	}

	request, _ := newValidPublishRequest(t, "test-room")
	response := httptest.NewRecorder()

	server.PublishHandler(response, request)

	assertResponse(t, response, http.StatusInternalServerError, "failed to create peer connection\n")

	if room, ok := server.findRoom("test-room"); ok || room != nil {
		t.Error("expected failed peer connection creation to release the room reservation")
	}
}

func TestPublishHandlerStoresPeerConnectionOnReservedRoom(t *testing.T) {
	server := NewServer()

	expectedPeerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create test peer connection: %v", err)
	}
	t.Cleanup(func() {
		expectedPeerConnection.Close()
	})

	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		return expectedPeerConnection, nil
	}

	request, _ := newValidPublishRequest(t, "test-room")
	response := httptest.NewRecorder()

	server.PublishHandler(response, request)

	room, ok := server.findRoom("test-room")
	if !ok {
		t.Fatal("expected reserved room to remain registered")
	}

	if room.publisherPeerConnection != expectedPeerConnection {
		t.Error("expected room to store the publisher peer connection")
	}
}

func TestPublishHandlerConfiguresPeerConnectionToReceiveAudioAndVideo(t *testing.T) {
	server := NewServer()

	expectedPeerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("failed to create test peer connection: %v", err)
	}
	t.Cleanup(func() {
		expectedPeerConnection.Close()
	})

	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		return expectedPeerConnection, nil
	}

	request, _ := newValidPublishRequest(t, "test-room")
	response := httptest.NewRecorder()

	server.PublishHandler(response, request)
	transceivers := expectedPeerConnection.GetTransceivers()

	if len(transceivers) != 2 {
		t.Fatalf("expected 2 transceivers, got %d", len(transceivers))
	}

	foundAudio := false
	foundVideo := false
	for _, transceiver := range transceivers {
		if transceiver.Kind() == webrtc.RTPCodecTypeAudio &&
			transceiver.Direction() == webrtc.RTPTransceiverDirectionRecvonly {
			foundAudio = true
		}

		if transceiver.Kind() == webrtc.RTPCodecTypeVideo &&
			transceiver.Direction() == webrtc.RTPTransceiverDirectionRecvonly {
			foundVideo = true
		}
	}

	if !foundAudio {
		t.Error("expected a receive-only audio transceiver")
	}

	if !foundVideo {
		t.Error("expected a receive-only video transceiver")
	}
}

func TestPublishHandlerSetsBrowserOfferAsRemoteDescription(t *testing.T) {
	server := NewServer()
	request, offer := newValidPublishRequest(t, "test-room")
	response := httptest.NewRecorder()

	server.PublishHandler(response, request)

	room, ok := server.findRoom("test-room")
	if !ok {
		t.Fatal("expected reserved room to remain registered")
	}

	remoteDescription := room.publisherPeerConnection.RemoteDescription()
	if remoteDescription == nil {
		t.Fatal("expected publisher peer connection to have a remote description")
	}

	if remoteDescription.Type != webrtc.SDPTypeOffer {
		t.Errorf("expected remote description type %q, got %q", webrtc.SDPTypeOffer, remoteDescription.Type)
	}

	if remoteDescription.SDP != offer.SDP {
		t.Error("expected remote description to contain the browser offer SDP")
	}
}

func newValidPublishRequest(t *testing.T, roomID string) (*http.Request, webrtc.SessionDescription) {
	t.Helper()

	const offerSDP = "v=0\r\n" +
		"o=- 0 0 IN IP4 127.0.0.1\r\n" +
		"s=-\r\n" +
		"t=0 0\r\n" +
		"a=group:BUNDLE 0 1\r\n" +
		"a=ice-ufrag:test\r\n" +
		"a=ice-pwd:testtesttesttesttesttest\r\n" +
		"a=fingerprint:sha-256 40:42:FB:47:87:52:BF:CB:EC:3A:DF:EB:06:DA:2D:B7:2F:59:42:10:23:7B:9D:4C:C9:58:DD:FF:A2:8F:17:67\r\n" +
		"m=video 9 UDP/TLS/RTP/SAVPF 96\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=setup:actpass\r\n" +
		"a=mid:0\r\n" +
		"a=sendonly\r\n" +
		"a=rtcp-mux\r\n" +
		"a=rtpmap:96 VP8/90000\r\n" +
		"m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=setup:actpass\r\n" +
		"a=mid:1\r\n" +
		"a=sendonly\r\n" +
		"a=rtcp-mux\r\n" +
		"a=rtpmap:111 opus/48000/2\r\n"

	offer := webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: offerSDP}

	body, err := json.Marshal(publishRequest{
		SDP:  offer.SDP,
		Type: offer.Type.String(),
	})
	if err != nil {
		t.Fatalf("failed to encode publish request: %v", err)
	}

	return newPublishRequest(roomID, string(body)), offer
}

func newPublishRequest(roomID, body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/publish/test-room", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("room", roomID)
	return request
}

func assertResponse(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantBody string) {
	t.Helper()

	if response.Code != wantStatus {
		t.Errorf("expected status %d, got %d", wantStatus, response.Code)
	}

	if got := response.Body.String(); got != wantBody {
		t.Errorf("expected body %q, got %q", wantBody, got)
	}
}
