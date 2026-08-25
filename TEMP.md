
  1. Read the room ID from the URL.

  2. Validate the room ID.
     Reject invalid IDs with 400 Bad Request.

  3. Check that Content-Type is application/json.
     Reject other formats with 415 Unsupported Media Type.

  4. Limit the request-body size.
     Prevent a client from sending an excessively large body.

  5. Decode the JSON body into an SDP offer.
     Reject malformed JSON or an invalid offer with 400 Bad Request.

  6. Reserve the room for this publisher.
     Reject an existing publisher with 409 Conflict.

  7. Create a server-side Pion PeerConnection.

  8. Configure it to receive audio and video.

  9. Set the browser's offer as the remote description.

  10. Create and set the server's SDP answer.

  11. Wait for server-side ICE gathering.
      Stop waiting on completion, timeout, or request cancellation.

  12. Encode the completed answer as JSON.
      Return 200 OK with Content-Type: application/json.
