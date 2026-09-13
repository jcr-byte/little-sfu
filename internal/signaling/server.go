package signaling

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type Room struct {
	ID                      string
	mu                      sync.RWMutex
	publisherPeerConnection *webrtc.PeerConnection
	audioTrack              *webrtc.TrackLocalStaticRTP
	videoTrack              *webrtc.TrackLocalStaticRTP
	viewers                 map[*webrtc.PeerConnection]struct{}
}

type Server struct {
	mu                sync.RWMutex
	rooms             map[string]*Room
	newPeerConnection func() (*webrtc.PeerConnection, error)
}

func NewServer() *Server {
	return &Server{
		rooms: make(map[string]*Room),
		newPeerConnection: func() (*webrtc.PeerConnection, error) {
			return webrtc.NewPeerConnection(webrtc.Configuration{})
		},
	}
}

func (s *Server) reserveRoom(roomID string) (*Room, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.rooms[roomID]
	if exists {
		return nil, false
	}

	room := &Room{
		ID:      roomID,
		viewers: make(map[*webrtc.PeerConnection]struct{}),
	}
	s.rooms[roomID] = room

	return room, true
}

func (s *Server) findRoom(roomID string) (*Room, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	room, exists := s.rooms[roomID]
	return room, exists
}

func (s *Server) removeRoom(roomID string, room *Room) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	existingRoom, exists := s.rooms[roomID]
	if !exists {
		return false
	}

	if existingRoom != room {
		return false
	}

	delete(s.rooms, roomID)
	return true
}

func (room *Room) addViewer(pc *webrtc.PeerConnection) {
	room.mu.Lock()
	defer room.mu.Unlock()

	room.viewers[pc] = struct{}{}
}

func (room *Room) removeViewer(pc *webrtc.PeerConnection) {
	room.mu.Lock()
	_, exists := room.viewers[pc]
	delete(room.viewers, pc)
	room.mu.Unlock()

	if exists {
		pc.Close()
	}
}
