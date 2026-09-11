package signaling

import "net/http"

func (server *Server) WatchHandler(w http.ResponseWriter, r *http.Request) {
	roomID := r.PathValue("room")

	room, exists := server.findRoom(roomID)
	if !exists {
		http.Error(w, "publisher is not ready", http.StatusConflict)
		return
	}

	room.mu.RLock()
	audioTrack := room.audioTrack
	videoTrack := room.videoTrack
	room.mu.RUnlock()

	if audioTrack == nil || videoTrack == nil {
		http.Error(w, "publisher is not ready", http.StatusConflict)
		return
	}
}
