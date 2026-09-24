package signaling

import (
	"errors"
	"fmt"
	"log"

	"github.com/pion/webrtc/v4"
)

func (room *Room) publishTrack(owner *Participant, track *webrtc.TrackLocalStaticRTP) error {
	room.mu.Lock()

	if room.closed || room.participants[owner.ID] != owner {
		room.mu.Unlock()
		return errors.New("publisher is no longer in the room")
	}

	owner.publishedTracks = append(owner.publishedTracks, track)

	recipients := make([]*Participant, 0, len(room.participants))
	for _, participant := range room.participants {
		if participant != owner {
			recipients = append(recipients, participant)
		}
	}

	room.mu.Unlock()

	var failures []error

	for _, recipient := range recipients {
		if err := recipient.negotiator.AddTrack(track); err != nil {
			failures = append(failures, fmt.Errorf("subscribe participant %s: %w", recipient.ID, err))
		}
	}

	return errors.Join(failures...)
}

func forwardRTP(remote *webrtc.TrackRemote, outgoing *webrtc.TrackLocalStaticRTP) error {
	for {
		packet, _, err := remote.ReadRTP()
		if err != nil {
			return err
		}

		if err := outgoing.WriteRTP(packet); err != nil {
			log.Printf("forward track %q: %v", outgoing.ID(), err)
		}
	}
}
