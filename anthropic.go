package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// AnthropicBackend uses the Anthropic API with ANTHROPIC_API_KEY.
type AnthropicBackend struct{}

func (b *AnthropicBackend) Name() string { return "anthropic" }

func (b *AnthropicBackend) Available() bool {
	return os.Getenv("ANTHROPIC_API_KEY") != ""
}

func (b *AnthropicBackend) Generate(ctx context.Context, instruction string) (string, error) {
	body := map[string]interface{}{
		"model":      "claude-3-5-haiku-latest",
		"max_tokens": 1024,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": instruction},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.anthropic.com/v1/messages",
		bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", os.Getenv("ANTHROPIC_API_KEY"))
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w (%s)", err, string(respBody))
	}

	if len(result.Content) == 0 {
		return "", fmt.Errorf("Anthropic returned empty response")
	}

	return result.Content[0].Text, nil
}
