// Package ollama provides a minimal JSON client for a local Ollama server.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxErrorBodyBytes caps how much of an error response we read into memory.
const maxErrorBodyBytes = 4 << 10

// Client calls JSON endpoints on an Ollama server.
type Client struct {
	baseURL string
	http    *http.Client
}

// New returns a Client for the server at baseURL, issuing requests through
// httpClient.
func New(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    httpClient,
	}
}

// PostJSON sends payload to path and decodes the JSON reply into out.
func (c *Client) PostJSON(ctx context.Context, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", path, err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build %s request: %w", path, err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("call ollama %s: %w", path, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))

		return fmt.Errorf(
			"ollama %s returned %s: %s",
			path,
			response.Status,
			strings.TrimSpace(string(detail)),
		)
	}

	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decode %s response: %w", path, err)
	}

	return nil
}
