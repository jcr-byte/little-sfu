package signaling

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestServerCloseCleansUpAllRooms(t *testing.T) {
	server := NewServer()
	publishers := make(map[string]*webrtc.PeerConnection)
	participants := make(map[string]*Participant)

	for _, roomID := range []string{"first-room", "second-room"} {
		room, reserved := server.reserveRoom(roomID)
		if !reserved {
			t.Fatalf("failed to reserve room %q", roomID)
		}

		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })

		room.publisherPeerConnection = pc
		publishers[roomID] = pc

		participantPC, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { participantPC.Close() })
		participant := &Participant{ID: "alice", pc: participantPC}
		if err := room.addParticipant(participant); err != nil {
			t.Fatalf("room %q: failed to add participant: %v", roomID, err)
		}
		participants[roomID] = participant
	}

	server.Close()

	for roomID, pc := range publishers {
		if state := pc.ConnectionState(); state != webrtc.PeerConnectionStateClosed {
			t.Errorf("room %q: publisher is %s, want closed", roomID, state)
		}

		if _, exists := server.findRoom(roomID); exists {
			t.Errorf("room %q: still registered after shutdown", roomID)
		}
		if state := participants[roomID].pc.ConnectionState(); state != webrtc.PeerConnectionStateClosed {
			t.Errorf("room %q: participant is %s, want closed", roomID, state)
		}
	}
}

func TestRemovePublisherClosesConnectionsAndReleasesRoom(t *testing.T) {
	server := NewServer()
	room, reserved := server.reserveRoom("test-room")
	if !reserved {
		t.Fatal("expected room reservation to succeed")
	}

	newConnection := func() *webrtc.PeerConnection {
		t.Helper()
		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		return pc
	}

	publisher := newConnection()
	room.publisherPeerConnection = publisher
	firstViewer := newConnection()
	secondViewer := newConnection()
	room.addViewer(firstViewer)
	room.addViewer(secondViewer)

	server.removePublisher(room)

	for name, pc := range map[string]*webrtc.PeerConnection{
		"publisher":     publisher,
		"first viewer":  firstViewer,
		"second viewer": secondViewer,
	} {
		if state := pc.ConnectionState(); state != webrtc.PeerConnectionStateClosed {
			t.Errorf("%s connection should be closed after publisher cleanup, got %s", name, state)
		}
	}

	if _, exists := server.findRoom(room.ID); exists {
		t.Error("expected publisher cleanup to remove the room")
	}
	replacement, reserved := server.reserveRoom(room.ID)
	if !reserved {
		t.Fatal("expected room ID to be reusable after publisher cleanup")
	}

	server.removePublisher(room)
	if found, exists := server.findRoom(room.ID); !exists || found != replacement {
		t.Error("expected repeated cleanup to preserve the replacement room")
	}
}

func TestAddViewerDoesNotRegisterViewerAfterPublisherCleanup(t *testing.T) {
	server := NewServer()
	room, reserved := server.reserveRoom("test-room")
	if !reserved {
		t.Fatal("expected room reservation to succeed")
	}

	viewer, err := server.newPeerConnection()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { viewer.Close() })

	// A watch request may still hold the room after publisher cleanup.
	server.removePublisher(room)
	if accepted := room.addViewer(viewer); accepted {
		t.Error("expected addViewer to return false for a closed room")
	}

	room.mu.RLock()
	_, registered := room.viewers[viewer]
	room.mu.RUnlock()

	if registered {
		t.Fatal("expected closed room to reject viewer registration")
	}
}

func TestRemoveViewerClosesConnectionAndPreservesOtherViewers(t *testing.T) {
	server := NewServer()
	room, _ := server.reserveRoom("test-room")

	newViewer := func() *webrtc.PeerConnection {
		t.Helper()
		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		room.addViewer(pc)
		return pc
	}
	viewer := newViewer()
	otherViewer := newViewer()

	for _, step := range []string{"first removal", "repeated removal"} {
		room.removeViewer(viewer)

		room.mu.RLock()
		_, stillRegistered := room.viewers[viewer]
		_, otherRegistered := room.viewers[otherViewer]
		room.mu.RUnlock()

		if stillRegistered {
			t.Errorf("%s: removed viewer is still registered", step)
		}
		if viewer.ConnectionState() != webrtc.PeerConnectionStateClosed {
			t.Errorf("%s: removed viewer connection is not closed", step)
		}
		if !otherRegistered || otherViewer.ConnectionState() == webrtc.PeerConnectionStateClosed {
			t.Errorf("%s: cleanup affected another viewer", step)
		}
	}
}

func TestAddParticipantSubscribesToExistingTracks(t *testing.T) {
	server := NewServer()
	t.Cleanup(server.Close)
	room := server.getOrCreateRoom("test-room")

	newParticipant := func(id string) *Participant {
		t.Helper()

		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })

		return &Participant{
			ID: id,
			pc: pc,
			negotiator: NewNegotiator(
				pc,
				func(webrtc.SessionDescription) error { return nil },
			),
		}
	}

	alice := newParticipant("alice")
	if err := room.addParticipant(alice); err != nil {
		t.Fatalf("failed to add Alice: %v", err)
	}

	audio := newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  2,
	}, "alice-audio")

	if err := room.publishTrack(alice, audio); err != nil {
		t.Fatalf("publish Alice's audio: %v", err)
	}

	bob := newParticipant("bob")
	if err := room.addParticipant(bob); err != nil {
		t.Fatalf("failed to add Bob: %v", err)
	}

	for _, sender := range bob.pc.GetSenders() {
		if sender.Track() == audio {
			return
		}
	}
	t.Fatal("Bob joined without subscribing to Alice's existing audio")
}

func TestRemoveParticipantRemovesTheirTracksFromOtherParticipants(t *testing.T) {
	server := NewServer()
	t.Cleanup(server.Close)
	room := server.getOrCreateRoom("test-room")

	newParticipant := func(id string) *Participant {
		t.Helper()

		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })

		return &Participant{
			ID: id,
			pc: pc,
			negotiator: NewNegotiator(
				pc,
				func(webrtc.SessionDescription) error { return nil },
			),
		}
	}

	alice := newParticipant("alice")
	if err := room.addParticipant(alice); err != nil {
		t.Fatalf("add Alice: %v", err)
	}

	audio := newWatchTestTrack(t, webrtc.RTPCodecCapability{
		MimeType:  webrtc.MimeTypeOpus,
		ClockRate: 48000,
		Channels:  2,
	}, "alice-audio")

	if err := room.publishTrack(alice, audio); err != nil {
		t.Fatalf("publish Alice's audio: %v", err)
	}

	bob := newParticipant("bob")
	if err := room.addParticipant(bob); err != nil {
		t.Fatalf("add Bob: %v", err)
	}

	hasAliceAudio := func() bool {
		for _, sender := range bob.pc.GetSenders() {
			if sender.Track() == audio {
				return true
			}
		}
		return false
	}

	// Ensure removal cannot pass simply because Bob never subscribed.
	if !hasAliceAudio() {
		t.Fatal("setup: Bob is not subscribed to Alice's audio")
	}

	server.removeParticipant(room, alice)

	if hasAliceAudio() {
		t.Error("Bob still has Alice's audio attached after Alice left")
	}
	if bob.pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
		t.Error("removing Alice's audio closed Bob's connection")
	}
}

func TestRemoveParticipantPreservesOtherParticipantsAndRoom(t *testing.T) {
	server := NewServer()
	room, reserved := server.reserveRoom("test-room")
	if !reserved {
		t.Fatal("expected room reservation to succeed")
	}

	newParticipant := func(id string) *Participant {
		t.Helper()
		pc, err := server.newPeerConnection()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { pc.Close() })
		participant := &Participant{ID: id, pc: pc}
		if err := room.addParticipant(participant); err != nil {
			t.Fatalf("failed to add participant %q: %v", id, err)
		}
		return participant
	}

	alice := newParticipant("alice")
	bob := newParticipant("bob")
	bobStateBefore := bob.pc.ConnectionState()

	server.removeParticipant(room, alice)

	if state := alice.pc.ConnectionState(); state != webrtc.PeerConnectionStateClosed {
		t.Errorf("Alice's connection is %s, want closed", state)
	}
	room.mu.RLock()
	_, alicePresent := room.participants[alice.ID]
	remainingBob := room.participants[bob.ID]
	roomClosed := room.closed
	room.mu.RUnlock()

	if alicePresent {
		t.Error("Alice is still registered after leaving")
	}
	if remainingBob != bob {
		t.Error("Alice leaving removed or replaced Bob")
	}
	if state := bob.pc.ConnectionState(); state != bobStateBefore {
		t.Errorf("Bob's connection changed from %s to %s", bobStateBefore, state)
	}
	if roomClosed {
		t.Error("Alice leaving closed the room")
	}
	if found, exists := server.findRoom(room.ID); !exists || found != room {
		t.Error("Alice leaving removed or replaced the room")
	}
}

func TestReserveRoom(t *testing.T) {
	server := NewServer()

	roomID := "123"
	room, reserved := server.reserveRoom(roomID)

	if !reserved {
		t.Fatal("expected reservation to succeed")
	}

	if room == nil {
		t.Fatal("expected room, got nil")
	}

	if room.ID != roomID {
		t.Errorf("expected room ID %q, got %q", roomID, room.ID)
	}
}

func TestReserveRoomRejectsDuplicate(t *testing.T) {
	server := NewServer()

	original, reserved := server.reserveRoom("123")
	if !reserved {
		t.Fatal("expected first reservation to succeed")
	}

	duplicate, reserved := server.reserveRoom("123")
	if reserved {
		t.Error("expected duplicate reservation to fail")
	}

	if duplicate != nil {
		t.Errorf("expected no room for duplicate reservation, got %#v", duplicate)
	}

	found, ok := server.findRoom("123")
	if !ok {
		t.Fatal("expected original room to remain registered")
	}

	if found != original {
		t.Error("expected duplicate reservation to preserve the original room")
	}
}

func TestFindRoom(t *testing.T) {
	server := NewServer()
	original, reserved := server.reserveRoom("123")
	if !reserved {
		t.Fatal("expected reservation to succeed")
	}

	found, ok := server.findRoom("123")
	if !ok {
		t.Fatal("expected to find reserved room")
	}

	if found != original {
		t.Error("expected findRoom to return the reserved room")
	}
}

func TestFindRoomReportsMissingRoom(t *testing.T) {
	server := NewServer()

	room, ok := server.findRoom("missing")
	if ok {
		t.Error("expected missing room lookup to fail")
	}

	if room != nil {
		t.Errorf("expected no room for missing lookup, got %#v", room)
	}
}

func TestRemoveRoom(t *testing.T) {
	server := NewServer()
	room, reserved := server.reserveRoom("123")
	if !reserved {
		t.Fatal("expected reservation to succeed")
	}

	removed := server.removeRoom("123", room)
	if !removed {
		t.Error("expected room removal to succeed")
	}

	found, ok := server.findRoom("123")
	if ok || found != nil {
		t.Error("expected removed room to be absent")
	}
}

func TestRemoveRoomRejectsDifferentRoomInstance(t *testing.T) {
	server := NewServer()
	original, reserved := server.reserveRoom("123")
	if !reserved {
		t.Fatal("expected reservation to succeed")
	}

	staleRoom := &Room{ID: "123"}
	removed := server.removeRoom("123", staleRoom)
	if removed {
		t.Error("expected removal with a different room instance to fail")
	}

	found, ok := server.findRoom("123")
	if !ok {
		t.Fatal("expected original room to remain registered")
	}

	if found != original {
		t.Error("expected failed removal to preserve the original room")
	}
}
