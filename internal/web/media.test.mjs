import test from "node:test";
import assert from "node:assert/strict";
import { createRemoteTrackHandler } from "./media.mjs";

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
