package signaling

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestNegotiatorWaitsForAnswerBeforeSendingQueuedOffer(t *testing.T) {
	newConnection := func() *webrtc.PeerConnection {
		t.Helper()
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		return pc
	}
	serverPC := newConnection()
	browserPC := newConnection()

	_, err := serverPC.AddTransceiverFromKind(
		webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	)
	if err != nil {
		t.Fatal(err)
	}

	// Record outgoing signaling messages instead of opening a WebSocket.
	// Requests and answers are processed synchronously by this interface.
	var offers []webrtc.SessionDescription
	var remoteDescriptionAtSecondOffer *webrtc.SessionDescription
	negotiator := NewNegotiator(serverPC, func(offer webrtc.SessionDescription) error {
		offers = append(offers, offer)
		if len(offers) == 2 {
			remoteDescriptionAtSecondOffer = serverPC.RemoteDescription()
		}
		return nil
	})

	if err := negotiator.RequestNegotiation(); err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 {
		t.Fatalf("initial request sent %d offers, want 1", len(offers))
	}

	if err := negotiator.RequestNegotiation(); err != nil {
		t.Fatal(err)
	}
	if len(offers) != 1 {
		t.Fatalf("sent %d offers before receiving an answer, want 1", len(offers))
	}

	// Generate a real answer without needing to establish a media connection.
	if err := browserPC.SetRemoteDescription(offers[0]); err != nil {
		t.Fatal(err)
	}
	answer, err := browserPC.CreateAnswer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := browserPC.SetLocalDescription(answer); err != nil {
		t.Fatal(err)
	}

	if err := negotiator.ApplyAnswer(answer); err != nil {
		t.Fatal(err)
	}
	if len(offers) != 2 {
		t.Fatalf("sent %d offers after applying the answer, want 2", len(offers))
	}
	if offers[1].Type != webrtc.SDPTypeOffer {
		t.Errorf("queued message type is %s, want offer", offers[1].Type)
	}
	if remoteDescriptionAtSecondOffer == nil ||
		remoteDescriptionAtSecondOffer.Type != webrtc.SDPTypeAnswer ||
		remoteDescriptionAtSecondOffer.SDP != answer.SDP {
		t.Error("queued offer was sent before the first answer was applied")
	}
}
