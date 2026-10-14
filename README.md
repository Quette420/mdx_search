# mdx

[![CI](https://github.com/Quette420/go_rag/actions/workflows/ci.yml/badge.svg)](https://github.com/Quette420/go_rag/actions/workflows/ci.yml)

Fast local search and an overview map for a folder of Markdown notes — a
research journal, an Obsidian vault, project docs, a reverse-engineering
log. One static binary, zero dependencies (Go standard library only),
fully offline unless you opt into LLM summaries.

```
$ mdx search AllocateChannelSequence

1. networking/connection-handshake.md:19 =
   Channel allocation
   status=done tags=networking,protocol
   `AllocateChannelSequence` runs right after `ClientConfirm`. Sequence numbers start at…
```

## Features

- **Hybrid ranking.** BM25 over an inverted index fused (RRF) with a literal
  pass, so identifiers and hex addresses like `0xF90410` are never drowned out
  by lexically similar neighbours. Literal hits are marked with `=`.
- **Code-aware tokenizer.** `CamelCase`, `snake_case` and letter/digit
  boundaries are split, so `stReplicatedStateFSM` is found by `replicated`,
  `state` or `fsm`. Light stemming for English and Russian.
- **Heading-aware chunks.** Results point to `file:line` with the full heading
  path; `#` comments inside fenced code are not mistaken for headings.
- **Frontmatter filters.** Any YAML field becomes a filter:
  `-f status=open -f tags=networking`.
- **Incremental indexing.** Only files whose SHA-256 changed are re-parsed —
  cheap enough for a git hook or a timer.
- **Digest.** Generates `INDEX.md`, a browsable map of the corpus grouped by
  folder or by any frontmatter field, optionally with LLM summaries.
- **Scriptable.** `-json` output for editors, scripts and LLM tools.

## Install

```sh
go install github.com/Quette420/go_rag/cmd/mdx@latest
```

Or build from source:

```sh
git clone https://github.com/Quette420/go_rag.git
cd go_rag
go build -o mdx ./cmd/mdx          # or: make build
```

Requires Go 1.22+.

## Quick start

```sh
cd ~/notes
mdx index                                   # builds .mdx/index.gob
mdx search "retry backoff"
mdx search 0x4A1F20 -full                   # print the whole chunk
mdx search "cache" -f status=rejected       # frontmatter filter
mdx search "handshake" -path networking -since 30d
mdx digest                                  # writes INDEX.md
mdx stats
```

Try it on the bundled sample corpus:

```sh
mdx index -root examples/notes
mdx search -root examples/notes "AllocateChannelSequence"
```

## Commands

| Command | What it does |
|---|---|
| `mdx index` | Index (or incrementally re-index) the corpus. `-ext .md,.mdx` selects extensions, `-exclude` skips files by glob or path substring. |
| `mdx search "query"` | Ranked search. `-k` results, `-f key=value` filters (repeatable), `-path`, `-since`, `-show` fields to print, `-full`, `-json`. |
| `mdx digest` | Write a corpus map. `-out` (`-` = stdout), `-group field` (default: top-level folder), `-show`, `-since`, `-title`. |
| `mdx stats` | Corpus size and value counts per frontmatter field (`-field` to pick fields). |

All commands accept `-root` (default `.`) and `-db` (default
`<root>/.mdx/index.gob`). `-since` accepts `2026-01-15`, `7d`, `3w` or `2m`.
Flags may go before or after the query.

## Frontmatter

mdx reads a minimal YAML subset — scalars, `[a, b]` lists and `- item` lists.
Nothing is required; a few fields have special meaning:

```yaml
---
title: Write-through cache hypothesis   # otherwise the first H1, then the file name
date: 2026-02-18                        # also: updated, created; else file mtime
status: rejected                        # any field works as a filter
tags: [storage, cache]
---
```

Keeping states like `status: rejected` in frontmatter, rather than in prose,
turns them into a queryable set: `mdx search cache -f status=rejected`.

## LLM summaries (optional)

```sh
export ANTHROPIC_API_KEY=...
mdx digest -summarize                    # summarize new/changed notes
mdx digest -summarize -limit 20          # cap the number of API calls
mdx digest -summarize -prompt my.txt     # custom prompt, one %s for the note
```

Summaries are cached in the index by file hash, so only changed notes are sent
again. This is the only network call in the program.

## How it works

```
cmd/mdx/              entry point
internal/
  cli/                command parsing and output
  markdown/           frontmatter, heading-aware chunking, outline
  textproc/           tokenizer, identifier splitting, stemming
  index/              incremental crawl, inverted index, gob storage
  search/             BM25 + literal pass, RRF fusion, snippets
  digest/             INDEX.md rendering
  summarize/          Anthropic Messages API client (net/http)
```

1. **Crawl** — walk the root (skipping hidden dirs, `node_modules`, `vendor`,
   …), hash each file, re-parse only changed ones.
2. **Chunk** — split each note by headings; oversized sections are split at
   paragraph or line boundaries (~2000 runes) so tables and long lists don't
   become one giant chunk.
3. **Index** — tokenize breadcrumb + path + body into postings. Deleted chunks
   are tombstoned and the postings are rebuilt once tombstones exceed 25%.
4. **Search** — rank with BM25 and, separately, by literal occurrences of the
   query and its identifier-like terms (candidates narrowed via the rarest
   token, so this stays exact and fast); fuse both lists with reciprocal rank
   fusion.

## Development

```sh
make test    # go test ./...
make vet
make demo    # build and search the sample corpus
```
