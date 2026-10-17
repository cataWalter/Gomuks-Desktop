package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	aiCompletionTimeout = 60 * time.Second
	aiHealthTimeout     = 10 * time.Second
	aiMaxResponseBytes  = 4 << 20
)

// AIMessage is one chat turn sent to the local model.
type AIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type aiChatRequest struct {
	Model    string      `json:"model,omitempty"`
	Messages []AIMessage `json:"messages"`
	Stream   bool        `json:"stream"`
}

type aiChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func validateAIEndpoint(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("AI endpoint must be an http or https URL")
	}
	return nil
}

// aiChatCompletion posts to an OpenAI-compatible chat endpoint such as llama-server.
func aiChatCompletion(ctx context.Context, client *http.Client, endpoint, model string, messages []AIMessage) (string, error) {
	if err := validateAIEndpoint(endpoint); err != nil {
		return "", err
	}
	if len(messages) == 0 {
		return "", errors.New("AI request has no messages")
	}
	body, err := json.Marshal(aiChatRequest{Model: model, Messages: messages, Stream: false})
	if err != nil {
		return "", fmt.Errorf("encode AI request: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, aiCompletionTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create AI request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("contact AI endpoint: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, aiMaxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read AI response: %w", err)
	}
	var parsed aiChatResponse
	decodeErr := json.Unmarshal(data, &parsed)
	if response.StatusCode < 200 || response.StatusCode > 299 {
		if decodeErr == nil && parsed.Error != nil && parsed.Error.Message != "" {
			return "", fmt.Errorf("AI endpoint returned %d: %s", response.StatusCode, parsed.Error.Message)
		}
		return "", fmt.Errorf("AI endpoint returned %d", response.StatusCode)
	}
	if decodeErr != nil {
		return "", fmt.Errorf("decode AI response: %w", decodeErr)
	}
	if len(parsed.Choices) == 0 {
		return "", errors.New("AI endpoint returned no choices")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}

// aiHealthCheck reports whether the server is up and the model has finished loading.
func aiHealthCheck(ctx context.Context, client *http.Client, endpoint string) error {
	if err := validateAIEndpoint(endpoint); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, aiHealthTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/health", nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("contact AI endpoint: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, aiMaxResponseBytes))
	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusServiceUnavailable:
		return errors.New("AI model is still loading")
	default:
		return fmt.Errorf("AI endpoint health check returned %d", response.StatusCode)
	}
}

func (s *AppService) aiEndpointSettings() (string, string, error) {
	preferences := s.preferences.All()
	endpoint, _ := preferences["ai.endpoint"].(string)
	model, _ := preferences["ai.model"].(string)
	if endpoint == "" {
		return "", "", errors.New("set an AI endpoint in Settings > Integrations")
	}
	return endpoint, model, nil
}

func (s *AppService) aiContext() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}

// AIComplete sends chat messages to the configured local endpoint and returns the reply text.
func (s *AppService) AIComplete(messages []AIMessage) (string, error) {
	endpoint, model, err := s.aiEndpointSettings()
	if err != nil {
		return "", err
	}
	return aiChatCompletion(s.aiContext(), aiHTTPClient, endpoint, model, messages)
}

// TestAIEndpoint checks that the configured endpoint is reachable and its model is loaded.
func (s *AppService) TestAIEndpoint() error {
	endpoint, _, err := s.aiEndpointSettings()
	if err != nil {
		return err
	}
	return aiHealthCheck(s.aiContext(), aiHTTPClient, endpoint)
}

var aiHTTPClient = &http.Client{}
