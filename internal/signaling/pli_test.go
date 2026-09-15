package signaling

import (
	"context"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestPublisherReceivesPeriodicPLI(t *testing.T) {
	// Set up the SFU and a shared deadline for negotiation and feedback.
	server := NewServer()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	// This connection plays the browser's role: publishing video to the SFU.
	publisher, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("Create publisher connection: %v", err)
	}
	t.Cleanup(func() {
		publisher.Close()
	})

	// Create a VP8 track; packets will be written to it after negotiation.
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
		"video",
		"test-stream",
	)
	if err != nil {
		t.Fatal(err)
	}

	// Keep the sender so we can read RTCP feedback from the SFU.
	videoSender, err := publisher.AddTrack(videoTrack)
	if err != nil {
		t.Fatalf("failed to add track: %v", err)
	}

	// Register SFU cleanup before negotiation, which may fail partway through.
	t.Cleanup(func() {
		room, found := server.findRoom("pli-test")
		if found {
			server.removePublisher(room)
		}
	})

	// Exchange a real SDP offer and answer through the publish handler.
	negotiateForwardingPeer(t, ctx, publisher, server.PublishHandler, "/publish/", "pli-test")

	// Report sending errors to the main test and wait for the sender at cleanup.
	writeErrors := make(chan error, 1)
	senderDone := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-senderDone
	})

	// Send synthetic RTP repeatedly so the SFU discovers an active video stream.
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		defer close(senderDone)

		var sequence uint16
		var timestamp uint32
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sequence += 1
				timestamp += 1800 // 20 ms on VP8's 90,000 Hz clock.
				packet := rtp.Packet{
					Header: rtp.Header{
						Version:        2,
						SequenceNumber: sequence,
						Timestamp:      timestamp,
					},
					Payload: []byte{0x10, 0x00, 0x01, 0x02, 0x03},
				}

				if err := videoTrack.WriteRTP(&packet); err != nil {
					writeErrors <- err
					return
				}
			}
		}
	}()

	// Collect keyframe requests and reading errors for the main test to check.
	receivedPLIs := make(chan *rtcp.PictureLossIndication, 2)
	readErrors := make(chan error, 1)
	readerFinished := make(chan struct{})
	// Closing the connection unblocks ReadRTCP; cancellation alone cannot.
	t.Cleanup(func() {
		cancel()
		publisher.Close()
		<-readerFinished
	})

	// Read feedback in the background because ReadRTCP blocks until data arrives.
	go func() {
		defer close(readerFinished)

		for {
			packets, _, err := videoSender.ReadRTCP()
			if err != nil {
				select {
				case readErrors <- err:
				case <-ctx.Done():
				}
				return
			}

			// RTCP also carries reports and other feedback; keep only PLIs.
			for _, packet := range packets {
				pli, ok := packet.(*rtcp.PictureLossIndication)
				if !ok {
					continue
				}

				select {
				case receivedPLIs <- pli:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	params := videoSender.GetParameters()
	if len(params.Encodings) == 0 {
		t.Fatal("expected a video encoding")
	}

	expectedSSRC := uint32(params.Encodings[0].SSRC)

	// Require two PLIs: an initial request alone doesn't prove requests repeat.
	receivedCount := 0
	for receivedCount < 2 {
		select {
		case pli := <-receivedPLIs:
			if pli.MediaSSRC != expectedSSRC {
				t.Fatalf("PLI targets SSRC %d, want %d", pli.MediaSSRC, expectedSSRC)
			}
			receivedCount++
		case err := <-readErrors:
			t.Fatalf("failed to read RTCP feedback: %v", err)
		case err := <-writeErrors:
			t.Fatalf("failed to send video RTP: %v", err)
		case <-ctx.Done():
			t.Fatalf("timed out: received %d of 2 PLIs", receivedCount)
		}
	}
}
