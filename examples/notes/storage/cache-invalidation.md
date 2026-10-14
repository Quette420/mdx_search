---
title: Write-through cache hypothesis
status: rejected
tags: [storage, cache]
date: 2026-02-18
---

We assumed the cache replicated every write to disk synchronously. Profiling
showed writes are batched every 500 ms, so the hypothesis was rejected.

## Evidence

The flush routine at `0x4A1F20` is called from a timer, not from the write path.
