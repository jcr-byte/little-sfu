package signaling

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/pion/webrtc/v4"
)

func drainRTCP(sender *webrtc.RTPSender) {
	for {
		_, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
	}
}

func (server *Server) WatchHandler(w http.ResponseWriter, r *http.Request) {
	// Find the requested room
	roomID := r.PathValue("room")

	room, exists := server.findRoom(roomID)
	if !exists {
		http.Error(w, "publisher is not ready", http.StatusConflict)
		return
	}

	// Read the publisher's outgoing tracks under the room lock
	room.mu.RLock()
	audioTrack := room.audioTrack
	videoTrack := room.videoTrack
	room.mu.RUnlock()

	// require both audio and video before accepting a viewer
	if audioTrack == nil || videoTrack == nil {
		http.Error(w, "publisher is not ready", http.StatusConflict)
		return
	}

	// Decode the viewer's SDP offer
	var offer webrtc.SessionDescription

	err := json.NewDecoder(r.Body).Decode(&offer)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Create the server side of the viewer connection
	peerConnection, err := server.newPeerConnection()
	if err != nil {
		http.Error(w, "failed to create peer connection", http.StatusInternalServerError)
		return
	}

	if !room.addViewer(peerConnection) {
		peerConnection.Close()
		http.Error(w, "publisher is not ready", http.StatusConflict)
		return
	}

	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateClosed:
			room.removeViewer(peerConnection)
		}
	})

	// Attach the publisher's audio track to the viewer connection
	audioSender, err := peerConnection.AddTrack(audioTrack)
	if err != nil {
		room.removeViewer(peerConnection)
		http.Error(w, "failed to add audio track", http.StatusInternalServerError)
		return
	}

	// Attach the publisher's video track to the viewer connection
	videoSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		room.removeViewer(peerConnection)
		http.Error(w, "failed to add video track", http.StatusInternalServerError)
		return
	}

	// Read feedback from both senders without blocking signaling
	go drainRTCP(audioSender)
	go drainRTCP(videoSender)

	// Apply the viewer's SDP offer.
	err = peerConnection.SetRemoteDescription(offer)
	if err != nil {
		room.removeViewer(peerConnection)
		http.Error(w, "invalid SDP offer", http.StatusBadRequest)
		return
	}

	// Create the SDP answer.
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		room.removeViewer(peerConnection)
		http.Error(w, "failed to create SDP answer", http.StatusInternalServerError)
		return
	}

	// Prepare to wait for ICE gathering.
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Apply the server's SDP answer.
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		room.removeViewer(peerConnection)
		http.Error(w, "failed to set local description", http.StatusInternalServerError)
		return
	}

	// Wait for ICE gathering or request cancellation.
	select {
	case <-gatherComplete:
		completed := peerConnection.LocalDescription()

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(completed); err != nil {
			room.removeViewer(peerConnection)
			log.Printf("failed to write viewer SDP answer: %v", err)
		}

	case <-r.Context().Done():
		room.removeViewer(peerConnection)

		if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
			http.Error(w, "ICE gathering timed out", http.StatusGatewayTimeout)
		} else {
			http.Error(w, "request cancelled", http.StatusRequestTimeout)
		}
	}
}
