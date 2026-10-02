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
  const firstStream = { id: "participant-a" };
  const secondStream = { id: "participant-b" };
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

  const stream = { id: "participant-a" };

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
