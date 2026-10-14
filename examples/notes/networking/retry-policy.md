---
title: Retry policy for dropped packets
status: open
tags: [networking]
date: 2026-03-10
---

Open question: should retransmits back off exponentially or linearly when the
link is congested?

## Observations

Linear backoff recovers faster on short drops, but under sustained congestion
it keeps the queue full. Need traces from a lossy link before deciding.
