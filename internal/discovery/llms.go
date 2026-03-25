package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// LLMEndpoint describes a discovered local LLM provider.
type LLMEndpoint struct {
	Name    string
	BaseURL string
	Online  bool
	Models  []string
}

var defaultLLMProbes = []struct {
	name       string
	baseURL    string
	healthPath string
	modelField string // JSON field containing model list in health response
}{
	{"Ollama", "http://localhost:11434", "/api/tags", "models"},
	{"LM Studio", "http://localhost:1234", "/v1/models", "data"},
	{"Jan.ai", "http://localhost:1337", "/v1/models", "data"},
	{"LocalAI", "http://localhost:8080", "/v1/models", "data"},
	{"GPT4All", "http://localhost:4891", "/v1/models", "data"},
}

// DiscoverLLMs probes all known local LLM endpoints concurrently and returns the results.
func DiscoverLLMs(ctx context.Context) []LLMEndpoint {
	client := &http.Client{Timeout: 2 * time.Second}
	results := make([]LLMEndpoint, len(defaultLLMProbes))

	var wg sync.WaitGroup
	for i, probe := range defaultLLMProbes {
		wg.Add(1)
		go func(idx int, p struct {
			name       string
			baseURL    string
			healthPath string
			modelField string
		}) {
			defer wg.Done()
			ep := LLMEndpoint{Name: p.name, BaseURL: p.baseURL}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+p.healthPath, nil)
			if err != nil {
				results[idx] = ep
				return
			}
			resp, err := client.Do(req)
			if err != nil || resp.StatusCode != http.StatusOK {
				results[idx] = ep
				return
			}
			ep.Online = true
			ep.Models = parseModelList(resp, p.modelField)
			resp.Body.Close()
			results[idx] = ep
		}(i, probe)
	}
	wg.Wait()

	return results
}

// parseModelList extracts model names from an Ollama or OpenAI-compat /models response.
func parseModelList(resp *http.Response, field string) []string {
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	list, ok := raw[field].([]any)
	if !ok {
		return nil
	}

	var names []string
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// Ollama: model.name, OpenAI-compat: model.id
		if name, ok := m["name"].(string); ok {
			names = append(names, name)
		} else if id, ok := m["id"].(string); ok {
			names = append(names, id)
		}
	}
	return names
}

// LLMEndpointURL returns the OpenAI-compatible base URL for a given LLM.
func LLMEndpointURL(ep *LLMEndpoint) string {
	if ep.Name == "Ollama" {
		return fmt.Sprintf("%s/v1", ep.BaseURL)
	}
	return ep.BaseURL
}
