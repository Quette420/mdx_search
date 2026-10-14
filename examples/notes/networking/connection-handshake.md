---
title: Connection handshake
status: done
tags: [networking, protocol]
date: 2026-03-02
---

The client and server agree on a session key in three messages; the server
allocates a channel only after the key is confirmed.

## Message flow

1. `ClientHello` carries the protocol version and a random nonce.
2. `ServerHello` answers with its nonce and a signed key share.
3. `ClientConfirm` proves possession of the session key.

## Channel allocation

`AllocateChannelSequence` runs right after `ClientConfirm`. Sequence numbers
start at a random offset so that replayed packets from an older session are
rejected.

```python
# not a heading: fenced code is ignored by the chunker
seq = random.getrandbits(32)
```
