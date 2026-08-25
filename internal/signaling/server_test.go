package signaling

import (
	"testing"
)

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
