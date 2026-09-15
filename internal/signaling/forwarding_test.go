package signaling

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestPublisherMediaReachesViewer(t *testing.T) {
	server := NewServer()
	const roomID = "forwarding-test"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	var connections []*webrtc.PeerConnection
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		for _, pc := range connections {
			pc.Close()
		}
		workers.Wait()
	})

	// Track both client and SFU connections so failed tests also release them.
	server.newPeerConnection = func() (*webrtc.PeerConnection, error) {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		if err == nil {
			connections = append(connections, pc)
		}
		return pc, err
	}
	newConnection := func() *webrtc.PeerConnection {
		t.Helper()
		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		return pc
	}

	publisher := newConnection()
	audio := newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2,
	}, "audio")
	video := newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType: webrtc.MimeTypeVP8, ClockRate: 90000,
	}, "video")
	for _, track := range []*webrtc.TrackLocalStaticRTP{audio, video} {
		sender, err := publisher.AddTrack(track)
		if err != nil {
			t.Fatal(err)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			drainRTCP(sender)
		}()
	}
	negotiateForwardingPeer(t, ctx, publisher, server.PublishHandler, "/publish/", roomID)

	// Synthetic payloads test forwarding, not whether a browser can decode media.
	audioPayload := []byte{0xf8, 0xff, 0xfe}
	videoPayload := []byte{0x10, 0x00, 0x01, 0x02, 0x03}
	writeErrors := make(chan error, 1)
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		var sequence uint16
		var audioTimestamp, videoTimestamp uint32
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			sequence++
			audioTimestamp += 960
			videoTimestamp += 1800
			for _, media := range []struct {
				track     *webrtc.TrackLocalStaticRTP
				payload   []byte
				timestamp uint32
			}{
				{audio, audioPayload, audioTimestamp},
				{video, videoPayload, videoTimestamp},
			} {
				packet := &rtp.Packet{
					Header: rtp.Header{Version: 2, SequenceNumber: sequence,
						Timestamp: media.timestamp, Marker: true},
					Payload: media.payload,
				}
				if err := media.track.WriteRTP(packet); err != nil {
					writeErrors <- fmt.Errorf("write %s RTP: %w", media.track.ID(), err)
					return
				}
			}
		}
	}()

	// OnTrack creates the outgoing tracks only after publisher packets arrive.
	readyTicker := time.NewTicker(10 * time.Millisecond)
	defer readyTicker.Stop()
	for {
		room, exists := server.findRoom(roomID)
		if exists {
			room.mu.RLock()
			ready := room.audioTrack != nil && room.videoTrack != nil
			room.mu.RUnlock()
			if ready {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for SFU to receive publisher audio and video")
		case err := <-writeErrors:
			t.Fatal(err)
		case <-readyTicker.C:
		}
	}

	viewer := newConnection()
	type receivedPacket struct {
		kind    webrtc.RTPCodecType
		payload []byte
		err     error
	}
	received := make(chan receivedPacket, 2)
	viewer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
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
	for _, kind := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeAudio, webrtc.RTPCodecTypeVideo} {
		if _, err := viewer.AddTransceiverFromKind(kind, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			t.Fatal(err)
		}
	}
	negotiateForwardingPeer(t, ctx, viewer, server.WatchHandler, "/watch/", roomID)

	want := map[webrtc.RTPCodecType][]byte{
		webrtc.RTPCodecTypeAudio: audioPayload,
		webrtc.RTPCodecTypeVideo: videoPayload,
	}
	for len(want) > 0 {
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for forwarded media; missing tracks: %v", want)
		case err := <-writeErrors:
			t.Fatal(err)
		case result := <-received:
			if result.err != nil {
				t.Fatalf("read viewer %s RTP: %v", result.kind, result.err)
			}
			payload, expected := want[result.kind]
			if !expected {
				t.Fatalf("unexpected or duplicate viewer track: %s", result.kind)
			}
			if !bytes.Equal(result.payload, payload) {
				t.Fatalf("%s payload changed: got %x, want %x", result.kind, result.payload, payload)
			}
			delete(want, result.kind)
		}
	}
}

// Exchange real offers and answers through the handlers; media uses real sockets.
func negotiateForwardingPeer(t *testing.T, ctx context.Context, pc *webrtc.PeerConnection,
	handler http.HandlerFunc, path, roomID string,
) {
	t.Helper()
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("%s create offer: %v", path, err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("%s set local offer: %v", path, err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		t.Fatalf("%s timed out gathering client ICE candidates", path)
	}
	body, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path+roomID, bytes.NewReader(body)).WithContext(ctx)
	request.SetPathValue("room", roomID)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s negotiation: got HTTP %d: %s", path, response.Code, response.Body)
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(response.Body).Decode(&answer); err != nil {
		t.Fatalf("%s decode answer: %v", path, err)
	}
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("%s apply answer: %v", path, err)
	}
}
