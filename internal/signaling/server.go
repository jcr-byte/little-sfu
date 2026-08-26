package signaling

import (
	"sync"
)

type Room struct {
	ID string
}

type Server struct {
	mu    sync.RWMutex
	rooms map[string]*Room
}

func NewServer() *Server {
	return &Server{
		rooms: make(map[string]*Room),
	}
}

func (s *Server) reserveRoom(roomID string) (*Room, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.rooms[roomID]
	if exists {
		return nil, false
	}

	room := &Room{ID: roomID}
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
