// Package agent provides the runtime framework for AI agents that connect
// to the Cortex graph server, receive tasks, and execute them via LLMs.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/EBTURKgit/cortex/internal/config"
	"github.com/EBTURKgit/cortex/internal/logging"
)

// ============================================================
// LLM Abstraction
// ============================================================

// Message represents a single message in a conversation.
type Message struct {
	Role    string `json:"role"`    // "system", "user", "assistant"
	Content string `json:"content"`
}

// ChatOptions configures an LLM chat request.
type ChatOptions struct {
	Model       string
	Temperature float64
	MaxTokens   int
	Stream      bool
}

// ChatResponse contains the LLM's response and usage info.
type ChatResponse struct {
	Content string
	Usage   TokenUsage
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// LLMClient is the interface for interacting with language models.
type LLMClient interface {
	// Chat sends a chat completion request and returns the response.
	Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error)

	// Name returns a human-readable name for this client (e.g., "ollama/codellama:7b").
	Name() string
}

// ============================================================
// Ollama Client
// ============================================================

// OllamaClient communicates with a local Ollama instance.
type OllamaClient struct {
	endpoint string
	model    string
	client   *http.Client
}

// ollamaRequest is the request body for Ollama's chat API.
type ollamaRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Options  struct {
		Temperature float64 `json:"temperature"`
	} `json:"options,omitempty"`
	Stream bool `json:"stream"`
}

// ollamaResponse is the response from Ollama's chat API.
type ollamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	DoneReason string `json:"done_reason"`
	EvalCount  int    `json:"eval_count"`
	PromptEvalCount int `json:"prompt_eval_count"`
}

// NewOllamaClient creates a new Ollama LLM client.
func NewOllamaClient(endpoint, model string) *OllamaClient {
	logging.Debug("Creating Ollama client",
		map[string]interface{}{"endpoint": endpoint, "model": model})

	return &OllamaClient{
		endpoint: endpoint,
		model:    model,
		client: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

// Chat implements LLMClient for Ollama.
func (c *OllamaClient) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error) {
	defer logging.Trace("OllamaClient.Chat",
		map[string]interface{}{"model": c.model, "messages": len(messages)})()

	if opts == nil {
		opts = &ChatOptions{Temperature: 0.7, MaxTokens: 4096}
	}

	reqBody := ollamaRequest{
		Model:    c.model,
		Messages: messages,
		Stream:   false,
	}
	reqBody.Options.Temperature = opts.Temperature

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(respBody))
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	logging.Debug("Ollama response received",
		map[string]interface{}{
			"eval_count": ollamaResp.EvalCount,
			"prompt_eval_count": ollamaResp.PromptEvalCount,
			"done_reason": ollamaResp.DoneReason,
		})

	return &ChatResponse{
		Content: ollamaResp.Message.Content,
		Usage: TokenUsage{
			PromptTokens:     ollamaResp.PromptEvalCount,
			CompletionTokens: ollamaResp.EvalCount,
			TotalTokens:      ollamaResp.PromptEvalCount + ollamaResp.EvalCount,
		},
	}, nil
}

// Name returns the client identifier.
func (c *OllamaClient) Name() string {
	return fmt.Sprintf("ollama/%s", c.model)
}

// ============================================================
// OpenAI-Compatible Client
// ============================================================

// OpenAIClient communicates with OpenAI or any OpenAI-compatible API.
type OpenAIClient struct {
	endpoint string
	model    string
	apiKey   string
	client   *http.Client
}

// openAIMessage matches OpenAI's message format.
type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openAIChatRequest is the request body for OpenAI's chat API.
type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

// openAIChatResponse is the response from OpenAI's chat API.
type openAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// NewOpenAIClient creates a new OpenAI-compatible LLM client.
func NewOpenAIClient(endpoint, model, apiKey string) *OpenAIClient {
	logging.Debug("Creating OpenAI client",
		map[string]interface{}{"endpoint": endpoint, "model": model})

	if endpoint == "" {
		endpoint = "https://api.openai.com/v1"
	}

	return &OpenAIClient{
		endpoint: endpoint,
		model:    model,
		apiKey:   apiKey,
		client: &http.Client{
			Timeout: 300 * time.Second,
		},
	}
}

// Chat implements LLMClient for OpenAI-compatible APIs.
func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, opts *ChatOptions) (*ChatResponse, error) {
	defer logging.Trace("OpenAIClient.Chat",
		map[string]interface{}{"model": c.model, "messages": len(messages)})()

	if opts == nil {
		opts = &ChatOptions{Temperature: 0.7, MaxTokens: 4096}
	}

	// Convert messages
	apiMessages := make([]openAIMessage, len(messages))
	for i, m := range messages {
		apiMessages[i] = openAIMessage{Role: m.Role, Content: m.Content}
	}

	reqBody := openAIChatRequest{
		Model:       c.model,
		Messages:    apiMessages,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
		Stream:      false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp openAIChatResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(apiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	logging.Debug("OpenAI response received",
		map[string]interface{}{
			"model":    apiResp.Model,
			"tokens":   apiResp.Usage.TotalTokens,
			"finish":   apiResp.Choices[0].FinishReason,
		})

	return &ChatResponse{
		Content: apiResp.Choices[0].Message.Content,
		Usage: TokenUsage{
			PromptTokens:     apiResp.Usage.PromptTokens,
			CompletionTokens: apiResp.Usage.CompletionTokens,
			TotalTokens:      apiResp.Usage.TotalTokens,
		},
	}, nil
}

// Name returns the client identifier.
func (c *OpenAIClient) Name() string {
	return fmt.Sprintf("openai/%s", c.model)
}

// ============================================================
// LLM Client Factory
// ============================================================

// NewLLMClient creates the appropriate LLM client from config.
func NewLLMClient(cfg config.LLMConfig) (LLMClient, error) {
	logging.Debug("Creating LLM client",
		map[string]interface{}{"provider": cfg.Provider, "model": cfg.Model})

	switch cfg.Provider {
	case "ollama":
		endpoint := cfg.Endpoint
		if endpoint == "" {
			endpoint = "http://localhost:11434"
		}
		return NewOllamaClient(endpoint, cfg.Model), nil

	case "openai":
		return NewOpenAIClient(cfg.Endpoint, cfg.Model, cfg.APIKey), nil

	case "anthropic":
		return NewOpenAIClient("https://api.anthropic.com/v1", cfg.Model, cfg.APIKey), nil

	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s (supported: ollama, openai, anthropic)", cfg.Provider)
	}
}
