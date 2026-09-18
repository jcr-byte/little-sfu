package signaling

import (
	"testing"

	"github.com/pion/webrtc/v4"
)

func TestServerCloseCleansUpAllRooms(t *testing.T) {
	server := NewServer()
	publishers := make(map[string]*webrtc.PeerConnection)

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
	}

	server.Close()

	for roomID, pc := range publishers {
		if state := pc.ConnectionState(); state != webrtc.PeerConnectionStateClosed {
			t.Errorf("room %q: publisher is %s, want closed", roomID, state)
		}

		if _, exists := server.findRoom(roomID); exists {
			t.Errorf("room %q: still registered after shutdown", roomID)
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
