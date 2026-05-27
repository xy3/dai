package main

import "context"

type Backend interface {
	Name() string
	Available() bool
	Generate(ctx context.Context, instruction string) (string, error)
}

var backends = []Backend{
	&GeminiCLIBackend{},
	&GeminiGcloudBackend{},
	&GeminiAPIBackend{},
	&OpenAIBackend{},
	&AnthropicBackend{},
}

func detectBackend() Backend {
	for _, b := range backends {
		if b.Available() {
			return b
		}
	}
	return nil
}
