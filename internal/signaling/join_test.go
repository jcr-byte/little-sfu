package signaling

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/websocket"
)

func TestServerCloseClosesParticipantSignaling(t *testing.T) {
	server := NewServer()
	t.Cleanup(server.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /join/{room}", server.JoinHandler)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

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

	// Receiving the offer establishes that the participant has registered.
	var offer webrtc.SessionDescription
	if err := websocket.JSON.Receive(client, &offer); err != nil {
		t.Fatalf("receive initial offer: %v", err)
	}
	if offer.Type != webrtc.SDPTypeOffer {
		t.Fatalf("received description type %s, want offer", offer.Type)
	}

	server.Close()

	var message webrtc.SessionDescription
	err = websocket.JSON.Receive(client, &message)
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected server shutdown to close participant signaling, got %v", err)
	}
}

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

func TestJoinedParticipantReceivesNewAudioTrack(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	server := NewServer()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /join/{room}", server.JoinHandler)
	httpServer := httptest.NewServer(mux)
	var peers []*webrtc.PeerConnection
	var sockets []*websocket.Conn
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		for _, socket := range sockets {
			socket.Close()
		}
		for _, peer := range peers {
			peer.Close()
		}
		server.Close()
		httpServer.Close()
		workers.Wait()
	})

	// Exercise signaling only through /join, including subsequent offers.
	answerOffer := func(name string, pc *webrtc.PeerConnection, socket *websocket.Conn, stage string) {
		t.Helper()
		var offer webrtc.SessionDescription
		if err := websocket.JSON.Receive(socket, &offer); err != nil {
			t.Fatalf("%s receive %s offer: %v", name, stage, err)
		}
		if offer.Type != webrtc.SDPTypeOffer {
			t.Fatalf("%s received %s during %s, want offer", name, offer.Type, stage)
		}
		if err := pc.SetRemoteDescription(offer); err != nil {
			t.Fatalf("%s apply %s offer: %v", name, stage, err)
		}
		answer, err := pc.CreateAnswer(nil)
		if err != nil {
			t.Fatalf("%s create %s answer: %v", name, stage, err)
		}
		gathered := webrtc.GatheringCompletePromise(pc)
		if err := pc.SetLocalDescription(answer); err != nil {
			t.Fatalf("%s set %s answer: %v", name, stage, err)
		}
		select {
		case <-gathered:
		case <-ctx.Done():
			t.Fatalf("%s timed out gathering ICE during %s", name, stage)
		}
		if err := websocket.JSON.Send(socket, pc.LocalDescription()); err != nil {
			t.Fatalf("%s send %s answer: %v", name, stage, err)
		}
	}

	join := func(name string) (*webrtc.PeerConnection, *websocket.Conn, *webrtc.TrackLocalStaticRTP) {
		t.Helper()
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err != nil {
			t.Fatal(err)
		}
		peers = append(peers, pc)
		connected := make(chan struct{}, 1)
		pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
			if state == webrtc.PeerConnectionStateConnected {
				select {
				case connected <- struct{}{}:
				default:
				}
			}
		})
		// Both participants negotiate publishing capability, but only A sends
		// packets. No incoming track exists until that transmission begins.
		audio := newWatchTestTrack(t, webrtc.RTPCodecCapability{
			MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
		}, name+"-audio")
		sender, err := pc.AddTrack(audio)
		if err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			drainRTCP(sender)
		}()

		config, err := websocket.NewConfig(
			"ws"+strings.TrimPrefix(httpServer.URL, "http")+"/join/new-audio", httpServer.URL,
		)
		if err != nil {
			t.Fatal(err)
		}
		socket, err := config.DialContext(ctx)
		if err != nil {
			t.Fatalf("%s connect signaling: %v", name, err)
		}
		sockets = append(sockets, socket)
		deadline, _ := ctx.Deadline()
		if err := socket.SetDeadline(deadline); err != nil {
			t.Fatal(err)
		}
		answerOffer(name, pc, socket, "initial")
		select {
		case <-connected:
		case <-ctx.Done():
			t.Fatalf("%s timed out establishing initial WebRTC connection", name)
		}
		return pc, socket, audio
	}

	_, _, audio := join("A")
	bob, bobSocket, _ := join("B")
	type receivedPacket struct {
		kind    webrtc.RTPCodecType
		payload []byte
		err     error
	}
	received := make(chan receivedPacket, 1)
	bob.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		packet, _, err := track.ReadRTP()
		result := receivedPacket{kind: track.Kind(), err: err}
		if err == nil {
			result.payload = packet.Payload
		}
		select {
		case received <- result:
		case <-ctx.Done():
		}
	})

	// Repeat packets because the initial packets trigger OnTrack before B has
	// negotiated its subscription. This checks transport, not audio decoding.
	payload := []byte{0xf8, 0xff, 0xfe}
	writeErrors := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var sequence uint16
		var timestamp uint32
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			sequence++
			timestamp += 960
			if err := audio.WriteRTP(&rtp.Packet{
				Header:  rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: timestamp},
				Payload: payload,
			}); err != nil {
				writeErrors <- err
				return
			}
		}
	}()

	answerOffer("B", bob, bobSocket, "renegotiation")
	select {
	case result := <-received:
		if result.err != nil {
			t.Fatalf("B read forwarded RTP: %v", result.err)
		}
		if result.kind != webrtc.RTPCodecTypeAudio || !bytes.Equal(result.payload, payload) {
			t.Fatalf("B received %s payload %x, want audio payload %x", result.kind, result.payload, payload)
		}
	case err := <-writeErrors:
		t.Fatalf("A write audio RTP: %v", err)
	case <-ctx.Done():
		t.Fatal("B timed out receiving A's audio after renegotiation")
	}
}
