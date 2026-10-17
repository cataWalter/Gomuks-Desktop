package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateAIEndpointRequiresHTTPURL(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:8080", "https://ai.example.net/base"} {
		if err := validateAIEndpoint(endpoint); err != nil {
			t.Fatalf("validateAIEndpoint(%q) error = %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"localhost:8080", "ftp://example.net", "http://", "not a url"} {
		if err := validateAIEndpoint(endpoint); err == nil {
			t.Fatalf("validateAIEndpoint(%q) accepted invalid endpoint", endpoint)
		}
	}
}

func TestPreferenceStoreAcceptsAIEndpointSettings(t *testing.T) {
	store, err := NewPreferenceStore(filepath.Join(t.TempDir(), "preferences.json"))
	if err != nil {
		t.Fatalf("NewPreferenceStore() error = %v", err)
	}
	if err := store.Set("ai.endpoint", "http://localhost:8080"); err != nil {
		t.Fatalf("Set(ai.endpoint) error = %v", err)
	}
	if err := store.Set("ai.endpoint", ""); err != nil {
		t.Fatalf("Set(ai.endpoint) clearing error = %v", err)
	}
	if err := store.Set("ai.endpoint", "localhost:8080"); err == nil {
		t.Fatal("Set(ai.endpoint) accepted a URL without a scheme")
	}
	if err := store.Set("ai.model", "local-model.gguf"); err != nil {
		t.Fatalf("Set(ai.model) error = %v", err)
	}
	if err := store.Set("ai.model", 3); err == nil {
		t.Fatal("Set(ai.model) accepted a non-string value")
	}
	if err := store.Set("ai.translate_language", "Spanish"); err != nil {
		t.Fatalf("Set(ai.translate_language) error = %v", err)
	}
	if err := store.Set("ai.translate_language", strings.Repeat("x", 41)); err == nil {
		t.Fatal("Set(ai.translate_language) accepted an overlong value")
	}
}

func TestAIChatCompletionSendsMessagesAndReturnsReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			t.Errorf("request = %s %s, want POST /v1/chat/completions", r.Method, r.URL.Path)
		}
		var body struct {
			Model    string      `json:"model"`
			Messages []AIMessage `json:"messages"`
			Stream   bool        `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if body.Model != "local.gguf" || body.Stream || len(body.Messages) != 1 || body.Messages[0].Content != "Translate hello" {
			t.Errorf("unexpected body: %+v", body)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"  Hola  "}}]}`)
	}))
	defer server.Close()

	reply, err := aiChatCompletion(context.Background(), server.Client(), server.URL+"/", "local.gguf", []AIMessage{{Role: "user", Content: "Translate hello"}})
	if err != nil {
		t.Fatalf("aiChatCompletion() error = %v", err)
	}
	if reply != "Hola" {
		t.Fatalf("reply = %q, want %q", reply, "Hola")
	}
}

func TestAIChatCompletionReportsServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"error":{"message":"Loading model"}}`)
	}))
	defer server.Close()

	_, err := aiChatCompletion(context.Background(), server.Client(), server.URL, "", []AIMessage{{Role: "user", Content: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "Loading model") {
		t.Fatalf("aiChatCompletion() error = %v, want server message", err)
	}
}

func TestAIChatCompletionRejectsEmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[]}`)
	}))
	defer server.Close()

	if _, err := aiChatCompletion(context.Background(), server.Client(), server.URL, "", []AIMessage{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatal("aiChatCompletion() accepted a response without choices")
	}
}

func TestAIChatCompletionRejectsEmptyMessages(t *testing.T) {
	if _, err := aiChatCompletion(context.Background(), http.DefaultClient, "http://localhost:8080", "", nil); err == nil {
		t.Fatal("aiChatCompletion() accepted an empty message list")
	}
}

func TestAIHealthCheckReportsLoadingModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %s, want /health", r.URL.Path)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := aiHealthCheck(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "still loading") {
		t.Fatalf("aiHealthCheck() error = %v, want loading message", err)
	}
}

func TestAIHealthCheckAcceptsReadyServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer server.Close()

	if err := aiHealthCheck(context.Background(), server.Client(), server.URL); err != nil {
		t.Fatalf("aiHealthCheck() error = %v", err)
	}
}
