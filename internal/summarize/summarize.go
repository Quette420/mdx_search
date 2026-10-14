// Package summarize fills per-file summaries using the Anthropic Messages API.
// It is the only part of mdx that talks to the network, and it talks to it via
// plain net/http to keep the binary dependency-free.
package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Quette420/mdx_search/internal/index"
)

const (
	DefaultModel = "claude-sonnet-5"
	apiURL       = "https://api.anthropic.com/v1/messages"
	apiVersion   = "2023-06-01"
	maxInput     = 24000 // bytes of note text sent per request
)

// DefaultPrompt is used when Options.Prompt is empty. %s receives the note.
const DefaultPrompt = `Below is a Markdown note from a personal knowledge base.
Write a 2-3 sentence summary: what the note is about, what conclusion it reaches,
and whether anything is left open. Write in the same language as the note.
No preamble, no markdown, just the summary. Do not invent facts that are not in the note.

---
%s`

type Options struct {
	APIKey string
	Model  string
	Prompt string    // must contain a single %s
	Limit  int       // max files per run; 0 = all
	Log    io.Writer // progress output; nil = silent
}

// Run summarizes files whose content changed since their last summary and
// returns how many were updated. Partial progress is kept on error.
func Run(ctx context.Context, ix *index.Index, opt Options) (int, error) {
	if opt.APIKey == "" {
		opt.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if opt.APIKey == "" {
		return 0, fmt.Errorf("ANTHROPIC_API_KEY is not set")
	}
	if opt.Model == "" {
		opt.Model = DefaultModel
	}
	if opt.Prompt == "" {
		opt.Prompt = DefaultPrompt
	}
	if strings.Count(opt.Prompt, "%s") != 1 {
		return 0, fmt.Errorf("prompt must contain exactly one %%s placeholder for the note")
	}

	var todo []*index.FileRec
	for _, f := range ix.Files {
		if f.SumHash != f.Hash {
			todo = append(todo, f)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].Path < todo[j].Path })
	if opt.Limit > 0 && len(todo) > opt.Limit {
		todo = todo[:opt.Limit]
	}

	c := &client{key: opt.APIKey, model: opt.Model, http: &http.Client{Timeout: 120 * time.Second}}
	done := 0
	for _, f := range todo {
		txt, err := c.complete(ctx, fmt.Sprintf(opt.Prompt, noteText(ix, f)))
		if err != nil {
			return done, fmt.Errorf("%s: %w", f.Path, err)
		}
		f.Summary, f.SumHash = strings.TrimSpace(txt), f.Hash
		done++
		if opt.Log != nil {
			fmt.Fprintf(opt.Log, "  summarized %s\n", f.Path)
		}
	}
	return done, nil
}

func noteText(ix *index.Index, f *index.FileRec) string {
	var b strings.Builder
	b.WriteString(f.Title + "\n\n" + f.Lead + "\n\n" + strings.Join(f.Headings, "\n"))
	for _, id := range f.ChunkIDs {
		if b.Len() > maxInput {
			break
		}
		if c := ix.Chunks[id]; c != nil {
			b.WriteString("\n\n" + c.Breadcrumb + "\n" + c.Body)
		}
	}
	return b.String()
}

type client struct {
	key, model string
	http       *http.Client
}

type apiReq struct {
	Model     string   `json:"model"`
	MaxTokens int      `json:"max_tokens"`
	Messages  []apiMsg `json:"messages"`
}

type apiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type apiResp struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *client) complete(ctx context.Context, prompt string) (string, error) {
	// Headroom for models that think before answering; the summary itself is short.
	payload, _ := json.Marshal(apiReq{
		Model: c.model, MaxTokens: 4096,
		Messages: []apiMsg{{Role: "user", Content: prompt}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out apiResp
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out.Error != nil {
		return "", fmt.Errorf("api (HTTP %d): %s", resp.StatusCode, out.Error.Message)
	}
	if out.StopReason == "refusal" {
		return "", fmt.Errorf("model declined to summarize this note")
	}
	var sb strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			sb.WriteString(blk.Text)
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("empty response (stop_reason=%s)", out.StopReason)
	}
	return sb.String(), nil
}
