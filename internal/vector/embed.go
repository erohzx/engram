package vector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder defines the interface for generating semantic embeddings from text.
type Embedder interface {
	// Embed returns a dense vector representation of the given text.
	// The caller is responsible for knowing the expected dimensionality.
	Embed(text string) ([]float32, error)

	// Dimension returns the embedding vector dimension for this model.
	Dimension() int
}

// OllamaEmbedder implements Embedder using the Ollama HTTP API.
type OllamaEmbedder struct {
	url  string
	model string
	client *http.Client
	dim  int
}

// ollamaEmbedRequest is the JSON body for Ollama /api/embeddings endpoint.
type ollamaEmbedRequest struct {
	Model string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedResponse is the JSON response from Ollama /api/embeddings endpoint.
type ollamaEmbedResponse struct {
	Model    string       `json:"model"`
	Embedding []float32   `json:"embedding"`
	TotalTime int64       `json:"total_time,omitempty"`
}

// NewOllamaEmbedder creates a new Ollama embedder.
// url should be like "http://localhost:11434" (no /api suffix).
// model should be like "nomic-embed-text:latest".
func NewOllamaEmbedder(url, model string) *OllamaEmbedder {
	// nomic-embed-text produces 768-dimensional vectors.
	const defaultDim = 768

	return &OllamaEmbedder{
		url:   url,
		model: model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		dim: defaultDim,
	}
}

// Embed generates a vector embedding for the given text using Ollama.
func (e *OllamaEmbedder) Embed(text string) ([]float32, error) {
	if text == "" {
		return nil, fmt.Errorf("ollama embed: empty input text")
	}

	req := ollamaEmbedRequest{
		Model:  e.model,
		Prompt: text,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := e.client.Post(e.url+"/api/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ollama response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ollama API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result ollamaEmbedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal ollama response: %w (body: %s)", err, string(respBody))
	}

	if len(result.Embedding) == 0 {
		return nil, fmt.Errorf("ollama returned empty embedding")
	}

	return result.Embedding, nil
}

// Dimension returns the embedding vector dimension (768 for nomic-embed-text).
func (e *OllamaEmbedder) Dimension() int {
	return e.dim
}
