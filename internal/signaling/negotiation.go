package signaling

import (
	"sync"

	"github.com/pion/webrtc/v4"
)

type Negotiator struct {
	pc             *webrtc.PeerConnection
	sendOffer      func(webrtc.SessionDescription) error
	awaitingAnswer bool
	pending        bool
	mu             sync.Mutex
}

func NewNegotiator(pc *webrtc.PeerConnection, sendOffer func(webrtc.SessionDescription) error) *Negotiator {
	return &Negotiator{
		pc:        pc,
		sendOffer: sendOffer,
	}
}

func (n *Negotiator) ApplyAnswer(answer webrtc.SessionDescription) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	err := n.pc.SetRemoteDescription(answer)
	if err != nil {
		return err
	}

	n.awaitingAnswer = false

	if n.pending {
		n.pending = false
		return n.requestNegotiationLocked()
	}

	return nil
}

func (n *Negotiator) AddTrack(track webrtc.TrackLocal) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	sender, err := n.pc.AddTrack(track)
	if err != nil {
		return err
	}

	go drainRTCP(sender)

	return n.requestNegotiationLocked()
}

func (n *Negotiator) RequestNegotiation() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	return n.requestNegotiationLocked()
}

func (n *Negotiator) requestNegotiationLocked() error {
	if n.awaitingAnswer {
		n.pending = true
		return nil
	}

	offer, err := n.pc.CreateOffer(nil)
	if err != nil {
		return err
	}

	if err = n.pc.SetLocalDescription(offer); err != nil {
		return err
	}

	n.awaitingAnswer = true
	return n.sendOffer(offer)
}
