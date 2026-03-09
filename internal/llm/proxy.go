package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/fleetq/fleetq-bridge/internal/tunnel"
)

// Proxy forwards LLM inference requests to local OpenAI-compatible endpoints.
type Proxy struct {
	client *http.Client
}

// NewProxy creates a new LLM proxy.
func NewProxy() *Proxy {
	return &Proxy{
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Forward sends a chat completion request to the specified local endpoint
// and streams chunks back via the provided channel.
func (p *Proxy) Forward(ctx context.Context, req *tunnel.LLMRequest, send func(*tunnel.LLMResponseChunk) error) error {
	// Build OpenAI-compatible request body
	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   req.Stream,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return err
	}

	// Determine endpoint URL
	endpointURL := req.EndpointURL
	if endpointURL == "" {
		return fmt.Errorf("no endpoint URL in LLM request")
	}
	url := endpointURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("LLM request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("LLM endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	if req.Stream {
		return p.streamResponse(ctx, req.RequestID, resp.Body, send)
	}

	return p.bufferResponse(ctx, req.RequestID, resp.Body, send)
}

// streamResponse reads an SSE stream and sends chunks.
func (p *Proxy) streamResponse(_ context.Context, requestID string, body io.Reader, send func(*tunnel.LLMResponseChunk) error) error {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "data: [DONE]" {
			continue
		}
		if len(line) < 6 || line[:6] != "data: " {
			continue
		}

		var chunk openAIChunk
		if err := json.Unmarshal([]byte(line[6:]), &chunk); err != nil {
			continue
		}

		delta := ""
		if len(chunk.Choices) > 0 {
			delta = chunk.Choices[0].Delta.Content
		}

		done := len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason == "stop"
		if err := send(&tunnel.LLMResponseChunk{
			RequestID: requestID,
			Delta:     delta,
			Done:      done,
		}); err != nil {
			return err
		}
		if done {
			return nil
		}
	}

	// Signal end of stream
	return send(&tunnel.LLMResponseChunk{RequestID: requestID, Done: true})
}

// bufferResponse reads a non-streaming response and sends it as one chunk.
func (p *Proxy) bufferResponse(_ context.Context, requestID string, body io.Reader, send func(*tunnel.LLMResponseChunk) error) error {
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return err
	}

	text := ""
	if len(resp.Choices) > 0 {
		text = resp.Choices[0].Message.Content
	}

	if err := send(&tunnel.LLMResponseChunk{RequestID: requestID, Delta: text}); err != nil {
		return err
	}
	return send(&tunnel.LLMResponseChunk{RequestID: requestID, Done: true})
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}
