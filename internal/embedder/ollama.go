package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// NomicEmbedTextDim is the vector dimension for the nomic-embed-text model.
const NomicEmbedTextDim = 768

// nomicEmbedTextMaxBytes is a conservative upper bound on input size for
// nomic-embed-text. The model's context window is ~8192 tokens; assuming a
// pessimistic ~4 bytes/token we cap inputs at ~30 KB to keep well under the
// limit even on code-heavy chunks with short tokens. Inputs longer than this
// are truncated by Embed before the API call so we don't waste a round-trip
// just to get a 400.
const nomicEmbedTextMaxBytes = 30 * 1024

type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllama(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{},
	}
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// Truncate inputs that would blow nomic-embed-text's ~8k-token context.
	// We do a defensive byte-cap rather than a real tokenizer pass — losing
	// the tail of a too-large chunk is preferable to silently failing the
	// upsert.
	if len(text) > nomicEmbedTextMaxBytes {
		text = text[:nomicEmbedTextMaxBytes]
	}
	body, err := json.Marshal(map[string]string{
		"model": o.model,
		"input": text,
	})
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Pass through Ollama's response body so the operator can see exactly
		// what went wrong (model name typo, context overflow, etc.).
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("ollama: unexpected status %d (input %d bytes): %s",
			resp.StatusCode, len(text), string(bytes.TrimSpace(respBody)))
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama: decode response: %w", err)
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("ollama: empty embeddings in response")
	}
	return result.Embeddings[0], nil
}
