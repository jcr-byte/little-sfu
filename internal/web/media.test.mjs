import test from "node:test";
import assert from "node:assert/strict";
import { createRemoteTrackHandler } from "./media.mjs";

test("two remote streams play in separate video elements", () => {
  const container = {
    children: [],
    appendChild(element) {
      this.children.push(element);
    },
  };
  const firstStream = createStream("participant-a");
  const secondStream = createStream("participant-b");
  const handleTrack = createRemoteTrackHandler({
    container,
    createVideo: () => ({ srcObject: null }),
  });

  handleTrack({ track: { kind: "video" }, streams: [firstStream] });
  handleTrack({ track: { kind: "video" }, streams: [secondStream] });

  assert.equal(container.children.length, 2, "each stream should have a video element");
  assert.notEqual(container.children[0], container.children[1], "streams should use distinct elements");
  assert.equal(container.children[0].srcObject, firstStream, "the first stream should remain displayed");
  assert.equal(container.children[1].srcObject, secondStream, "the second stream should be displayed separately");
});

test("audio and video from one stream share one video element", () => {
  const container = {
    children: [],
    appendChild(element) {
      this.children.push(element);
    }
  }

  const stream = createStream("participant-a");

  const handleTrack = createRemoteTrackHandler({
    container,
    createVideo: () => ({ srcObject: null }),
  });

  handleTrack({
    track: { kind: "audio" },
    streams: [stream],
  });

  handleTrack({
    track: { kind: "video"},
    streams: [stream],
  });

  assert.equal(
    container.children.length,
    1,
    "audio and video from the same stream should share one element",
  );

  assert.equal(
    container.children[0].srcObject,
    stream,
    "the video element should display the received stream",
  );
});

function createStream(id) {
  const stream = new EventTarget();
  stream.id = id;
  stream.tracks = [{ kind: "audio" }, { kind: "video" }];
  stream.getTracks = () => stream.tracks;
  stream.removeNextTrack = () => {
    stream.tracks.shift();
    stream.dispatchEvent(new Event("removetrack"));
  };
  return stream;
}

test("departure removes only the empty stream's tile and allows it to return", () => {
  const container = {
    children: [],
    appendChild(video) {
      this.children.push(video);
    },
  };
  const handleTrack = createRemoteTrackHandler({
    container,
    createVideo: () => ({
      srcObject: null,
      remove() {
        container.children.splice(container.children.indexOf(this), 1);
      },
    }),
  });
  const departing = createStream("departing");
  const remaining = createStream("remaining");
  handleTrack({ streams: [departing] });
  handleTrack({ streams: [remaining] });
  const departedVideo = container.children[0];
  const remainingVideo = container.children[1];

  departing.removeNextTrack();
  assert.equal(container.children.length, 2, "keep the tile while audio or video remains");
  departing.removeNextTrack();
  assert.deepEqual(container.children, [remainingVideo], "preserve other participants' tiles");
  assert.equal(departedVideo.srcObject, null, "release the departed stream");

  const returning = createStream("departing");
  handleTrack({ streams: [returning] });
  departing.dispatchEvent(new Event("removetrack"));
  assert.equal(container.children.length, 2, "old events must not remove another tile");
  assert.equal(container.children[1].srcObject, returning, "a returning stream gets a new tile");
});
