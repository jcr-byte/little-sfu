package signaling

import (
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
)

type Room struct {
	ID                      string
	mu                      sync.RWMutex
	publisherPeerConnection *webrtc.PeerConnection
	audioTrack              *webrtc.TrackLocalStaticRTP
	videoTrack              *webrtc.TrackLocalStaticRTP
	viewers                 map[*webrtc.PeerConnection]struct{}
	closed                  bool
	participants            map[string]*Participant
}

type Server struct {
	mu                sync.RWMutex
	rooms             map[string]*Room
	newPeerConnection func() (*webrtc.PeerConnection, error)
	gatheringTimeout  time.Duration
	gatheringComplete func(*webrtc.PeerConnection) <-chan struct{}
}

type Participant struct {
	ID string
	pc *webrtc.PeerConnection
}

func NewServer() *Server {
	return &Server{
		rooms:             make(map[string]*Room),
		newPeerConnection: newSFUPeerConnection,
		gatheringTimeout:  10 * time.Second,
		gatheringComplete: webrtc.GatheringCompletePromise,
	}
}

func (s *Server) Close() {
	s.mu.RLock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, room := range s.rooms {
		rooms = append(rooms, room)
	}
	s.mu.RUnlock()

	for _, room := range rooms {
		s.removePublisher(room)
	}
}

func newSFUPeerConnection() (*webrtc.PeerConnection, error) {
	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	registry := &interceptor.Registry{}

	// Enable RTCP sender and receiver reports.
	if err := webrtc.ConfigureRTCPReports(registry); err != nil {
		return nil, err
	}

	// Ask for a video keyframe approximately every three seconds.
	pli, err := intervalpli.NewReceiverInterceptor(
		intervalpli.GeneratorInterval(3 * time.Second),
	)
	if err != nil {
		return nil, err
	}
	registry.Add(pli)

	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
	)

	return api.NewPeerConnection(webrtc.Configuration{})
}

func (s *Server) reserveRoom(roomID string) (*Room, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.rooms[roomID]
	if exists {
		return nil, false
	}

	room := &Room{
		ID:           roomID,
		viewers:      make(map[*webrtc.PeerConnection]struct{}),
		participants: make(map[string]*Participant),
	}
	s.rooms[roomID] = room

	return room, true
}

func (s *Server) getOrCreateRoom(roomID string) *Room {
	s.mu.Lock()
	defer s.mu.Unlock()

	if room, exists := s.rooms[roomID]; exists {
		return room
	}

	room := &Room{
		ID:           roomID,
		viewers:      make(map[*webrtc.PeerConnection]struct{}),
		participants: make(map[string]*Participant),
	}
	s.rooms[roomID] = room

	return room
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

func (s *Server) removePublisher(room *Room) {
	room.mu.Lock()
	if room.closed {
		room.mu.Unlock()
		return
	}
	room.closed = true

	savedPublisher := room.publisherPeerConnection
	room.publisherPeerConnection = nil

	savedViewers := make([]*webrtc.PeerConnection, 0, len(room.viewers))
	for pc := range room.viewers {
		savedViewers = append(savedViewers, pc)
	}
	clear(room.viewers)
	savedParticipants := make([]*Participant, 0, len(room.participants))
	for _, participant := range room.participants {
		savedParticipants = append(savedParticipants, participant)
	}
	clear(room.participants)
	room.audioTrack = nil
	room.videoTrack = nil
	room.mu.Unlock()

	// Closing connections can trigger callbacks that need the room lock.
	if savedPublisher != nil {
		savedPublisher.Close()
	}
	for _, pc := range savedViewers {
		pc.Close()
	}
	for _, participant := range savedParticipants {
		participant.pc.Close()
	}
	s.removeRoom(room.ID, room)
}

func (room *Room) addParticipant(participant *Participant) bool {
	room.mu.Lock()
	defer room.mu.Unlock()

	if room.closed {
		return false
	}
	if _, exists := room.participants[participant.ID]; exists {
		return false
	}

	room.participants[participant.ID] = participant
	return true
}

func (s *Server) removeParticipant(room *Room, participant *Participant) {
	room.mu.Lock()
	// A delayed callback must not remove a replacement with the same ID.
	if room.participants[participant.ID] != participant {
		room.mu.Unlock()
		return
	}
	delete(room.participants, participant.ID)
	room.mu.Unlock()

	// Closing can trigger callbacks that need the room lock.
	participant.pc.Close()
}

func (room *Room) addViewer(pc *webrtc.PeerConnection) bool {
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.closed {
		return false
	}

	room.viewers[pc] = struct{}{}
	return true
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
