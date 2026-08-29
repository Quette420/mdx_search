package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

func metaFirst(m map[string][]string, key string) string {
	if v := m[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// BuildDigest renders a grouped map of the corpus: one block per file with
// status, date, outline and either the lead paragraph or an LLM summary.
func (ix *Index) BuildDigest(since time.Time, group string) string {
	type row struct{ f *FileRec }
	groups := map[string][]*FileRec{}
	for _, f := range ix.Files {
		if !since.IsZero() {
			t := fileDate(f)
			if t.IsZero() || t.Before(since) {
				continue
			}
		}
		g := metaFirst(f.Meta, group)
		if g == "" {
			g = "(без " + group + ")"
		}
		groups[g] = append(groups[g], f)
	}

	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("# Карта заметок\n\n")
	fmt.Fprintf(&b, "_Сгенерировано %s", time.Now().Format("2006-01-02 15:04"))
	if !since.IsZero() {
		fmt.Fprintf(&b, ", изменения с %s", since.Format("2006-01-02"))
	}
	total := 0
	for _, g := range names {
		total += len(groups[g])
	}
	fmt.Fprintf(&b, ". Файлов: %d._\n\n", total)

	for _, g := range names {
		fs := groups[g]
		sort.Slice(fs, func(i, j int) bool { return fileDate(fs[i]).After(fileDate(fs[j])) })
		fmt.Fprintf(&b, "## %s\n\n", g)
		for _, f := range fs {
			fmt.Fprintf(&b, "### [%s](%s)\n\n", f.Title, f.Path)
			var tags []string
			if s := metaFirst(f.Meta, "status"); s != "" {
				tags = append(tags, "статус: "+s)
			}
			if t := fileDate(f); !t.IsZero() {
				tags = append(tags, t.Format("2006-01-02"))
			}
			if o := f.Meta["offsets"]; len(o) > 0 {
				tags = append(tags, "offsets: "+strings.Join(o, ", "))
			}
			if len(tags) > 0 {
				fmt.Fprintf(&b, "`%s`\n\n", strings.Join(tags, " · "))
			}
			text := f.Summary
			if text == "" {
				text = f.Lead
			}
			if text != "" {
				fmt.Fprintf(&b, "%s\n\n", text)
			}
			if len(f.Headings) > 0 {
				n := len(f.Headings)
				if n > 12 {
					n = 12
				}
				for _, h := range f.Headings[:n] {
					fmt.Fprintf(&b, "- %s\n", h)
				}
				if len(f.Headings) > n {
					fmt.Fprintf(&b, "- …ещё %d\n", len(f.Headings)-n)
				}
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

const sumPrompt = `Ты помогаешь вести исследовательский журнал по реверс-инжинирингу.
Ниже — заметка в Markdown. Напиши 2–3 предложения по-русски: что именно исследовалось,
какой вывод получен и остался ли вопрос открытым. Без вступлений, без markdown,
только сам текст. Не выдумывай фактов, которых нет в заметке.

---
%s`

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
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Summarize fills FileRec.Summary for files whose content changed since the last
// run. Requires ANTHROPIC_API_KEY; without it digest falls back to lead paragraphs.
func (ix *Index) Summarize(model string, limit int, verbose bool) (int, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return 0, fmt.Errorf("не задан ANTHROPIC_API_KEY")
	}
	var todo []*FileRec
	for _, f := range ix.Files {
		if f.SumHash != f.Hash {
			todo = append(todo, f)
		}
	}
	sort.Slice(todo, func(i, j int) bool { return todo[i].Path < todo[j].Path })
	if limit > 0 && len(todo) > limit {
		todo = todo[:limit]
	}

	client := &http.Client{Timeout: 120 * time.Second}
	done := 0
	for _, f := range todo {
		body := f.Title + "\n\n" + f.Lead + "\n\n" + strings.Join(f.Headings, "\n")
		for _, id := range f.ChunkIDs {
			if c := ix.Chunks[id]; c != nil {
				body += "\n\n" + c.Breadcrumb + "\n" + c.Body
			}
			if len(body) > 24000 {
				break
			}
		}
		txt, err := callAPI(client, key, model, fmt.Sprintf(sumPrompt, body))
		if err != nil {
			return done, fmt.Errorf("%s: %w", f.Path, err)
		}
		f.Summary, f.SumHash = strings.TrimSpace(txt), f.Hash
		done++
		if verbose {
			fmt.Fprintf(os.Stderr, "  summarized %s\n", f.Path)
		}
	}
	return done, nil
}

func callAPI(c *http.Client, key, model, prompt string) (string, error) {
	payload, _ := json.Marshal(apiReq{
		Model: model, MaxTokens: 400,
		Messages: []apiMsg{{Role: "user", Content: prompt}},
	})
	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.Do(req)
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
		return "", fmt.Errorf("api: %s", out.Error.Message)
	}
	var sb strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			sb.WriteString(blk.Text)
		}
	}
	return sb.String(), nil
}
