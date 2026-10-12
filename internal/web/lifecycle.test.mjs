import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { runInNewContext } from "node:vm";
import { createOfferHandler } from "./signaling.mjs";
import { createRemoteTrackHandler } from "./media.mjs";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const script = html.match(/<script type="module">([\s\S]*?)<\/script>/)[1]
  .replace(/^\s*import .*;$/gm, "");

function setup() {
  const elements = new Map();
  for (const id of ["#join", "#leave", "#local-video", "#remote-videos"]) {
    elements.set(id, {
      disabled: id === "#leave",
      srcObject: null,
      handlers: {},
      addEventListener(type, handler) { this.handlers[type] = handler; },
      replaceChildren() {},
    });
  }
  const requests = [];
  const peers = [];
  const sockets = [];
  const errors = [];
  runInNewContext(script, {
    document: { querySelector: (id) => elements.get(id) },
    navigator: { mediaDevices: { getUserMedia() {
      const request = Promise.withResolvers();
      requests.push(request);
      return request.promise;
    } } },
    RTCPeerConnection: class {
      constructor() { peers.push(this); }
      addEventListener(type, handler) { this[type] = handler; }
      addTrack() {}
      close() { this.closed = true; }
    },
    WebSocket: class {
      constructor() { sockets.push(this); }
      close() { this.closed = true; }
    },
    location: { search: "?room=test", href: "http://localhost:8080/", protocol: "http:" },
    URL, URLSearchParams, createOfferHandler, createRemoteTrackHandler,
    console: { error: (...args) => errors.push(args) },
  });
  return {
    elements, requests, peers, sockets, errors,
    join: () => elements.get("#join").handlers.click(),
    leave: () => elements.get("#leave").handlers.click(),
  };
}

function media() {
  const track = { stopped: false, stop() { this.stopped = true; } };
  return { track, stream: { getTracks: () => [track] } };
}

test("leaving during camera permission prevents connections and stops acquired media", async () => {
  const app = setup();
  const joining = app.join();
  app.leave();
  const acquired = media();
  app.requests[0].resolve(acquired.stream);
  await joining;

  assert.equal(app.peers.length, 0, "must not open WebRTC after leaving");
  assert.equal(app.sockets.length, 0, "must not open signaling after leaving");
  assert.equal(acquired.track.stopped, true, "release newly acquired camera/microphone");
  assert.equal(app.elements.get("#local-video").srcObject, null);
  assert.equal(app.elements.get("#join").disabled, false);
  assert.equal(app.elements.get("#leave").disabled, true);
});

for (const outcome of ["resolve", "reject"]) {
  test(`an old permission request that ${outcome}s cannot disturb a new session`, async () => {
    const app = setup();
    const oldJoin = app.join();
    app.leave();
    const newJoin = app.join();
    const current = media();
    app.requests[1].resolve(current.stream);
    await newJoin;
    const stale = media();
    if (outcome === "resolve") app.requests[0].resolve(stale.stream);
    else app.requests[0].reject(new Error("Permission denied"));
    await oldJoin;

    assert.equal(app.peers.length, 1);
    assert.equal(app.sockets.length, 1);
    assert.equal(app.peers[0].closed, undefined, "preserve the current connection");
    assert.equal(current.track.stopped, false);
    assert.equal(app.elements.get("#local-video").srcObject, current.stream);
    assert.equal(app.elements.get("#join").disabled, true);
    assert.equal(app.elements.get("#leave").disabled, false);
    if (outcome === "resolve") assert.equal(stale.track.stopped, true);

    // Late callbacks from an old socket must also leave a new session alone.
    const oldError = app.sockets[0].onerror;
    const oldClose = app.sockets[0].onclose;
    app.leave();
    assert.equal(current.track.stopped, true);
    assert.equal(app.peers[0].closed, true);
    assert.equal(app.sockets[0].closed, true);
    const rejoining = app.join();
    const replacement = media();
    app.requests[2].resolve(replacement.stream);
    await rejoining;
    oldError();
    oldClose();
    assert.equal(app.peers[1].closed, undefined);
    assert.equal(replacement.track.stopped, false);
    assert.equal(app.elements.get("#local-video").srcObject, replacement.stream);
  });
}
