package signaling

import (
	"context"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"golang.org/x/net/websocket"
)

func (server *Server) JoinHandler(w http.ResponseWriter, r *http.Request) {
	// Reject invalid room IDs while we can still return an HTTP error.
	roomID := r.PathValue("room")

	if len(roomID) < 1 || len(roomID) > 64 {
		http.Error(w, "invalid room ID", http.StatusBadRequest)
		return
	}

	for _, char := range roomID {
		isAllowed :=
			('a' <= char && char <= 'z') ||
				('A' <= char && char <= 'Z') ||
				('0' <= char && char <= '9') ||
				char == '_' ||
				char == '-'

		if !isAllowed {
			http.Error(w, "invalid room ID", http.StatusBadRequest)
			return
		}
	}

	// Keep signaling open so tracks can be negotiated as the room changes.
	websocket.Handler(func(conn *websocket.Conn) {
		peerConnection, err := server.newPeerConnection()
		if err != nil {
			log.Printf("room %q failed to create participant peer connection: %v", roomID, err)
			return
		}
		defer peerConnection.Close()

		// Tie room membership to this signaling session; deferred cleanup handles exits.
		room := server.getOrCreateRoom(roomID)

		participant := &Participant{
			ID:             uuid.NewString(),
			pc:             peerConnection,
			closeSignaling: conn.Close,
		}

		// Offer to receive audio and video so the participant can publish both.
		_, err = peerConnection.AddTransceiverFromKind(
			webrtc.RTPCodecTypeAudio,
			webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionRecvonly,
			},
		)
		if err != nil {
			log.Printf("room %q failed to create participant audio transceiver: %v", roomID, err)
			return
		}

		_, err = peerConnection.AddTransceiverFromKind(
			webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{
				Direction: webrtc.RTPTransceiverDirectionRecvonly,
			},
		)
		if err != nil {
			log.Printf("room %q failed to create participant video transceiver: %v", roomID, err)
			return
		}

		// Register before negotiation starts ICE gathering via SetLocalDescription.
		gatherComplete := server.gatheringComplete(peerConnection)
		negotiator := NewNegotiator(peerConnection, func(_ webrtc.SessionDescription) error {
			ctx, cancel := context.WithTimeout(r.Context(), server.gatheringTimeout)
			defer cancel()

			select {
			case <-gatherComplete:
				return websocket.JSON.Send(conn, peerConnection.LocalDescription())
			case <-ctx.Done():
				return ctx.Err()
			}
		})

		participant.negotiator = negotiator

		// Handle each incoming track when its first RTP packets arrive.
		peerConnection.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
			// Prefix IDs so different publishers can use the same track and stream IDs.
			outgoing, err := webrtc.NewTrackLocalStaticRTP(
				remote.Codec().RTPCodecCapability,
				participant.ID+"-"+remote.ID(),
				participant.ID+"-"+remote.StreamID(),
			)
			if err != nil {
				log.Printf("room %q failed to create outgoing track: %v", roomID, err)
				return
			}

			if err := room.publishTrack(participant, outgoing); err != nil {
				log.Printf("room %q failed to publish track: %v", roomID, err)
			}

			// Keep forwarding for successful subscribers even if another failed.
			if err := forwardRTP(remote, outgoing); err != nil {
				log.Printf("room %q stopped forwarding track %q: %v", roomID, remote.ID(), err)
			}
		})

		if err := negotiator.RequestNegotiation(); err != nil {
			log.Printf("room %q failed to send participant offer: %v", roomID, err)
			return
		}

		if !room.addParticipant(participant) {
			log.Printf("room %q failed to register participant", roomID)
			return
		}
		defer server.removeParticipant(room, participant)

		// Process answers for both the initial connection and later track changes.
		for {
			var answer webrtc.SessionDescription
			if err := websocket.JSON.Receive(conn, &answer); err != nil {
				return
			}

			// The server creates offers, so clients must respond with answers.
			if answer.Type != webrtc.SDPTypeAnswer {
				log.Printf("room %q expected answer, received %s", roomID, answer.Type)
				return
			}

			if err := negotiator.ApplyAnswer(answer); err != nil {
				log.Printf("room %q failed to apply participant answer: %v", roomID, err)
				return
			}
		}
	}).ServeHTTP(w, r)
}
