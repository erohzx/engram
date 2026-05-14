// Package vector provides a Qdrant client wrapper for storing and searching
// semantic vectors generated from observations.
package vector

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
)

// ScoredPoint represents a Qdrant search result with its similarity score.
type ScoredPoint struct {
	ID     string               `json:"id"`
	Score  float64              `json:"score"`
	Payload map[string]any      `json:"payload"`
}

// Client is a thin HTTP wrapper around the Qdrant REST API.
// It handles batch upserts, point search, collection creation, and deletion.
type Client struct {
	url    string
	apiKey string
	client *http.Client
	mu     sync.Mutex
}

// NewClient creates a new Qdrant client.
// Set apiKey to empty string if no API key auth is needed (default for local dev).
func NewClient(url, apiKey string) *Client {
	return &Client{
		url:    url,
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 30 * 0,
		},
	}
}

// CreateCollection creates a new Qdrant collection with HNSW index.
func (c *Client) CreateCollection(ctx context.Context, name string, dim int, distance CollectionDistance) error {
	payload := map[string]any{
		"vectors": map[string]any{
			"size":     dim,
			"distance": string(distance),
		},
	}

	return c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/collections/%s", urlPathEscape(name)), payload, nil)
}

// UpsertPoint upserts a single point into a collection.
// pointID must be either an integer or UUID string as required by Qdrant.
func (c *Client) UpsertPoint(ctx context.Context, collection string, pointID any, vector []float32, payload map[string]any) error {
	vectors, err := serializeVector(vector)
	if err != nil {
		return fmt.Errorf("serialize vector: %w", err)
	}

	params := url.Values{}
	params.Set("wait", "true")

	body := map[string]any{
		"points": []map[string]any{
			{
				"id":      pointID,
				"vector":  vectors,
				"payload": payload,
			},
		},
	}

	return c.doJSON(ctx, http.MethodPut, fmt.Sprintf("/collections/%s/points?%s", urlPathEscape(collection), params.Encode()), body, nil)
}

// UpsertPointsBatch upserts multiple points in a single request.
func (c *Client) UpsertPointsBatch(ctx context.Context, collection string, points []Point) error {
	pointsMap := make([]map[string]any, 0, len(points))
	for _, p := range points {
		vectors, err := serializeVector(p.Vector)
		if err != nil {
			return fmt.Errorf("serialize vector for point %s: %w", p.ID, err)
		}

		pointsMap = append(pointsMap, map[string]any{
			"id":     p.ID,
			"vector": vectors,
			"payload": p.Payload,
		})
	}

	body := map[string]any{
		"points": pointsMap,
	}

	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/collections/%s/points?wait=true", urlPathEscape(collection)), body, nil)
}

// SearchPoints performs a vector similarity search against a collection.
func (c *Client) SearchPoints(ctx context.Context, collection string, query []float32, limit int, filter map[string]any) ([]ScoredPoint, error) {
	queryBody := map[string]any{
		"vector": serializeVectorRaw(query),
		"limit":  limit,
	}

	if filter != nil && len(filter) > 0 {
		queryBody["filter"] = filter
	}

	var resp struct {
		Points []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}

	err := c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/collections/%s/points/search", urlPathEscape(collection)), queryBody, &resp)
	if err != nil {
		return nil, err
	}

	results := make([]ScoredPoint, 0, len(resp.Points))
	for _, p := range resp.Points {
		idStr := fmt.Sprintf("%v", p.ID)
		results = append(results, ScoredPoint{
			ID:      idStr,
			Score:   p.Score,
			Payload: p.Payload,
		})
	}

	return results, nil
}

// DeletePoint removes a single point from a collection.
func (c *Client) DeletePoint(ctx context.Context, collection, pointID string) error {
	body := map[string]any{
		"points": []string{pointID},
	}

	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/collections/%s/points/keys?wait=true", urlPathEscape(collection)), body, nil)
}

// DeletePointsBatch removes multiple points from a collection.
func (c *Client) DeletePointsBatch(ctx context.Context, collection string, pointIDs []string) error {
	body := map[string]any{
		"points": pointIDs,
	}

	return c.doJSON(ctx, http.MethodPost, fmt.Sprintf("/collections/%s/points/keys?wait=true", urlPathEscape(collection)), body, nil)
}

// CollectionExists checks whether a collection exists.
func (c *Client) CollectionExists(ctx context.Context, name string) (bool, error) {
	var resp map[string]any
	err := c.doJSON(ctx, http.MethodGet, fmt.Sprintf("/collections/%s", urlPathEscape(name)), nil, &resp)
	if err != nil {
		if isNotFound(err) {
			return false, nil
		}
		return false, err
	}
	_, ok := resp["status"]
	return ok, nil
}

// ListCollections returns all collection names.
func (c *Client) ListCollections(ctx context.Context) ([]string, error) {
	var resp struct {
		Collections []struct {
			Name string `json:"name"`
		} `json:"collections"`
	}

	err := c.doJSON(ctx, http.MethodGet, "/collections", nil, &resp)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(resp.Collections))
	for _, c := range resp.Collections {
		names = append(names, c.Name)
	}

	return names, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// Point represents a single vector point for batch upsert.
type Point struct {
	ID      string
	Vector  []float32
	Payload map[string]any
}

// CollectionDistance defines the distance metric for vector comparison.
type CollectionDistance string

const (
	DistanceCosine    CollectionDistance = "Cosine"
	DistanceDotProduct CollectionDistance = "Dot"
	DistanceEuclidean  CollectionDistance = "Euclid"
)

// serializeVector converts []float32 to [][]float64 for Qdrant API.
func serializeVector(v []float32) ([]float64, error) {
	result := make([]float64, len(v))
	for i, f := range v {
		result[i] = float64(f)
	}
	return result, nil
}

// serializeVectorRaw returns the vector as a JSON-compatible slice for raw embedding.
func serializeVectorRaw(v []float32) []interface{} {
	result := make([]interface{}, len(v))
	for i, f := range v {
		result[i] = f
	}
	return result
}

// urlPathEscape escapes a collection name for use in URL paths.
func urlPathEscape(name string) string {
	escaped := make([]byte, 0, len(name)*2)
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			escaped = append(escaped, byte(r))
		} else {
			escaped = append(escaped, fmt.Sprintf("%%%02X", r)...)
		}
	}
	return string(escaped)
}

// isNotFound returns true if the error indicates a 404 Not Found.
func isNotFound(err error) bool {
	return err != nil && containsString(err.Error(), "404")
}

// doJSON performs an HTTP request with JSON body and optional response target.
func (c *Client) doJSON(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.url+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Api-Key", c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("qdrant API error %d: %s", resp.StatusCode, string(respBody))
	}

	if result == nil {
		return nil
	}

	if err := json.Unmarshal(respBody, result); err != nil {
		return fmt.Errorf("unmarshal response: %w (body: %s)", err, string(respBody))
	}

	return nil
}

// containsString is a simple string search helper.
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && searchInString(s, substr)
}

func searchInString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// serializeVectorToBlob converts []float32 to a binary blob for SQLite storage.
func serializeVectorToBlob(v []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		return nil, fmt.Errorf("serialize vector to blob: %w", err)
	}
	return buf.Bytes(), nil
}

// deserializeBlobToVector converts a binary blob back to []float32.
func deserializeBlobToVector(data []byte) ([]float32, error) {
	if len(data) == 0 {
		return nil, nil
	}
	v := make([]float32, len(data)/4)
	r := bytes.NewReader(data)
	if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
		return nil, fmt.Errorf("deserialize vector from blob: %w", err)
	}
	return v, nil
}
