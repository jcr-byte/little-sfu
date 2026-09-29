export function createRemoteTrackHandler({ container, createVideo }) {
  const videos = new Map();

  return function handleTrack(event) {
    const stream = event.streams[0];
    if (videos.has(stream.id)) {
      return;
    }

    const newVideo = createVideo();
    newVideo.srcObject = stream;
    container.appendChild(newVideo);
    videos.set(stream.id, newVideo);
  };
}
