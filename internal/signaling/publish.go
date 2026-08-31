package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"

	"github.com/pion/webrtc/v4"
)

type publishRequest struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
}

func (server *Server) PublishHandler(w http.ResponseWriter, r *http.Request) {
	// validate the request method is POST
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// validate room id
	roomID := r.PathValue("room")

	if len(roomID) < 1 || len(roomID) > 64 {
		http.Error(
			w,
			"invalid room ID",
			http.StatusBadRequest,
		)
		return
	}

	for _, char := range roomID {
		isAllowed :=
			('a' <= char && char <= 'z') ||
				('A' <= char && char <= 'Z') ||
				('0' <= char && char <= '9') ||
				char == '-' ||
				char == '_'

		if !isAllowed {
			http.Error(
				w,
				"invalid room ID",
				http.StatusBadRequest,
			)
			return
		}
	}

	// validate content type is application/json
	headerMediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		http.Error(
			w,
			"Content-Type must be application/json",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	if headerMediaType != "application/json" {
		http.Error(
			w,
			"Content-Type must be application/json",
			http.StatusUnsupportedMediaType,
		)
		return
	}

	// limit the request body size
	const maxBodyBytes = 64 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	decoder := json.NewDecoder(r.Body)

	var offer publishRequest
	err = decoder.Decode(&offer)
	if err != nil {
		var maxBytesErr *http.MaxBytesError

		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return

	}

	// ensure the body contains exactly one JSON value
	err = decoder.Decode(&struct{}{})
	if err != io.EOF {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}

		http.Error(w, "request body must contain exactly one JSON object", http.StatusBadRequest)
		return
	}

	// validate SDP
	if strings.TrimSpace(offer.SDP) == "" {
		http.Error(w, "SDP must not be empty", http.StatusBadRequest)
		return
	}

	// validate offer type
	if offer.Type != "offer" {
		http.Error(w, `type must be "offer"`, http.StatusBadRequest)
		return
	}

	// reserve room
	room, success := server.reserveRoom(roomID)
	if !success {
		http.Error(w, "room already has a publisher", http.StatusConflict)
		return
	}

	// create a server side pion connection
	peerConnection, err := server.newPeerConnection()
	if err != nil {
		server.removeRoom(roomID, room)
		http.Error(w, "failed to create peer connection", http.StatusInternalServerError)
		return
	}

	// configure a receive-only audio transceiver
	_, err = peerConnection.AddTransceiverFromKind(
		webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	)
	if err != nil {
		peerConnection.Close()
		server.removeRoom(roomID, room)
		http.Error(w, "failed to create audio transceiver", http.StatusInternalServerError)
		return
	}

	// configure a receive-only video transceiver
	_, err = peerConnection.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		},
	)
	if err != nil {
		peerConnection.Close()
		server.removeRoom(roomID, room)
		http.Error(w, "failed to create video transceiver", http.StatusInternalServerError)
		return
	}

	// set the remote description
	sessionDescription := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}
	err = peerConnection.SetRemoteDescription(sessionDescription)
	if err != nil {
		peerConnection.Close()
		server.removeRoom(roomID, room)
		http.Error(w, "invalid SDP offer", http.StatusBadRequest)
		return
	}

	// generate SDP answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		peerConnection.Close()
		server.removeRoom(roomID, room)
		log.Printf("failed to create SDP answer for room %q: %v", roomID, err)
		http.Error(w, "failed to create SDP answer", http.StatusInternalServerError)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// set local description
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		peerConnection.Close()
		server.removeRoom(roomID, room)
		log.Printf("failed to set local description for room %q: %v", roomID, err)
		http.Error(w, "failed to set local description", http.StatusInternalServerError)
		return
	}

	select {
	case <-gatherComplete:
		room.publisherPeerConnection = peerConnection
		completed := peerConnection.LocalDescription()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(completed); err != nil {
			log.Printf("failed to write SDP answer for room %q: %v", roomID, err)
		}
	case <-r.Context().Done():
		peerConnection.Close()
		server.removeRoom(roomID, room)

		switch r.Context().Err() {
		case context.DeadlineExceeded:
			http.Error(w, "ICE gathering timed out", http.StatusGatewayTimeout)
		default:
			http.Error(w, "request cancelled", http.StatusRequestTimeout)
		}
	}
}
