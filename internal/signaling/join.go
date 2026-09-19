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

	websocket.Handler(func(conn *websocket.Conn) {
		peerConnection, err := server.newPeerConnection()
		if err != nil {
			log.Printf("room %q failed to create participant peer connection: %v", roomID, err)
			return
		}
		defer peerConnection.Close()

		room := server.getOrCreateRoom(roomID)

		participant := &Participant{
			ID:             uuid.NewString(),
			pc:             peerConnection,
			closeSignaling: conn.Close,
		}

		if !room.addParticipant(participant) {
			log.Printf("room %q failed to register participant", roomID)
			return
		}
		defer server.removeParticipant(room, participant)

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
				// The completed description includes the gathered ICE candidates.
				return websocket.JSON.Send(conn, peerConnection.LocalDescription())
			case <-ctx.Done():
				return ctx.Err()
			}
		})

		if err := negotiator.RequestNegotiation(); err != nil {
			log.Printf("room %q failed to send participant offer: %v", roomID, err)
			return
		}

		for {
			var answer webrtc.SessionDescription
			if err := websocket.JSON.Receive(conn, &answer); err != nil {
				return
			}

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
