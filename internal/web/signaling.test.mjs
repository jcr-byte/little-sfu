import test from "node:test";
import assert from "node:assert/strict";
import { setImmediate } from "node:timers/promises";
import { createOfferHandler } from "./signaling.mjs";

test("second offer waits until the first answer is sent", async () => {
  const firstSendStarted = Promise.withResolvers();
  const allowFirstSend = Promise.withResolvers();
  const appliedOffers = [];
  const sentAnswers = [];

  const firstOffer = { type: "offer", sdp: "first-offer" };
  const secondOffer = { type: "offer", sdp: "second-offer" };

  // Fake the browser's WebRTC boundary; this test does not parse SDP.
  const pc = {
    async setRemoteDescription(offer) {
      appliedOffers.push(offer);
    },
    async createAnswer() {
      return { type: "answer", sdp: "fake-answer" };
    },
    async setLocalDescription(answer) {
      this.localDescription = answer;
    },
  };

  const handleOffer = createOfferHandler({
    pc,
    waitForIceGathering: async () => {},
    sendAnswer: async (answer) => {
      if (sentAnswers.length === 0) {
        firstSendStarted.resolve();
        await allowFirstSend.promise;
      }
      sentAnswers.push(answer);
    },
  });

  const first = handleOffer(firstOffer);
  await firstSendStarted.promise;

  const second = handleOffer(secondOffer);

  try {
    // Drain runnable promise callbacks while the first send stays blocked.
    await setImmediate();

    assert.deepEqual(appliedOffers, [firstOffer]);
    assert.equal(sentAnswers.length, 0);
  } finally {
    allowFirstSend.resolve();
    await Promise.all([first, second]);
  }

  assert.deepEqual(appliedOffers, [firstOffer, secondOffer]);
  assert.equal(sentAnswers.length, 2);
});
