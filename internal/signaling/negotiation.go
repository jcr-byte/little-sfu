package signaling

import "github.com/pion/webrtc/v4"

type Negotiator struct {
	pc             *webrtc.PeerConnection
	sendOffer      func(webrtc.SessionDescription) error
	awaitingAnswer bool
	pending        bool
}

func NewNegotiator(pc *webrtc.PeerConnection, sendOffer func(webrtc.SessionDescription) error) *Negotiator {
	return &Negotiator{
		pc:        pc,
		sendOffer: sendOffer,
	}
}

func (n *Negotiator) RequestNegotiation() error {
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

func (n *Negotiator) ApplyAnswer(answer webrtc.SessionDescription) error {
	err := n.pc.SetRemoteDescription(answer)
	if err != nil {
		return err
	}

	n.awaitingAnswer = false

	if n.pending {
		n.pending = false
		return n.RequestNegotiation()
	}

	return nil
}
