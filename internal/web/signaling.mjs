export function createOfferHandler({ pc, waitForIceGathering, sendAnswer }) {
  let pending = Promise.resolve()
 
  return function handleOffer(offer) {
    pending = pending.then(async () => {
      await pc.setRemoteDescription(offer)
      const answer = await pc.createAnswer();
      await pc.setLocalDescription(answer);
      await waitForIceGathering(pc);
      await sendAnswer(pc.localDescription);
    })
    return pending;
  }
}
