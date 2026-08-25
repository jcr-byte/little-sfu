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
	return nil, false
}
