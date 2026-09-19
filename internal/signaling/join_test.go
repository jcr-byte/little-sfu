package signaling

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"golang.org/x/net/websocket"
)

func TestJoinHandlerAppliesParticipantAnswer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	server := NewServer()
	t.Cleanup(server.Close)

	// Observe the real Pion connection accepting the answer, without polling
	// room state or replacing the negotiation coordinator with a mock.
	appliedAnswers := make(chan webrtc.SessionDescription, 1)
	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		pc, err := newSFUPeerConnection()
		if err != nil {
			return nil, err
		}
		t.Cleanup(func() { pc.Close() })
		pc.OnSignalingStateChange(func(state webrtc.SignalingState) {
			if state != webrtc.SignalingStateStable {
				return
			}
			answer := pc.RemoteDescription()
			if answer != nil && answer.Type == webrtc.SDPTypeAnswer {
				select {
				case appliedAnswers <- *answer:
				default:
				}
			}
		})
		return pc, nil
	}

	// Joining initiates a server offer; messages use the existing {type, sdp}
	// description format, now carried over a persistent WebSocket connection.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /join/{room}", server.JoinHandler)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	config, err := websocket.NewConfig(
		"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/join/test-room",
		httpServer.URL,
	)
	if err != nil {
		t.Fatal(err)
	}
	client, err := config.DialContext(ctx)
	if err != nil {
		t.Fatalf("connect participant signaling: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	deadline, _ := ctx.Deadline()
	if err := client.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}

	var offer webrtc.SessionDescription
	if err := websocket.JSON.Receive(client, &offer); err != nil {
		t.Fatalf("receive server offer: %v", err)
	}
	if offer.Type != webrtc.SDPTypeOffer {
		t.Fatalf("received description type %s, want offer", offer.Type)
	}

	browserPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { browserPC.Close() })
	if err := browserPC.SetRemoteDescription(offer); err != nil {
		t.Fatalf("apply server offer at participant: %v", err)
	}
	answer, err := browserPC.CreateAnswer(nil)
	if err != nil {
		t.Fatalf("create participant answer: %v", err)
	}
	if err := browserPC.SetLocalDescription(answer); err != nil {
		t.Fatalf("apply participant's local answer: %v", err)
	}
	if err := websocket.JSON.Send(client, answer); err != nil {
		t.Fatalf("send participant answer: %v", err)
	}

	select {
	case applied := <-appliedAnswers:
		if applied.SDP != answer.SDP {
			t.Error("server applied a different SDP answer from the one the participant sent")
		}
	case <-ctx.Done():
		t.Fatal("server did not apply the participant's answer and return to stable signaling")
	}
}
