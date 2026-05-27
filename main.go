package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const systemPrompt = `You are a shell command generator. Convert the user's instruction into a single shell command. Output ONLY the command, no explanation, no markdown formatting, no backticks. Just the raw command text. If you cannot determine the command, output "ERROR: " followed by a brief reason.`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: dai <instruction>")
		fmt.Fprintln(os.Stderr, "Example: dai \"create a merge request for this branch\"")
		os.Exit(1)
	}

	instruction := strings.Join(os.Args[1:], " ")

	backend := detectBackend()
	if backend == nil {
		fmt.Fprintln(os.Stderr, "dai: no AI backend found")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Install one of the following:")
		fmt.Fprintln(os.Stderr, "  - Gemini CLI:       npm install -g @google/gemini-cli")
		fmt.Fprintln(os.Stderr, "  - gcloud auth:      gcloud auth application-default login")
		fmt.Fprintln(os.Stderr, "  - Gemini API key:   export GEMINI_API_KEY=...")
		fmt.Fprintln(os.Stderr, "  - OpenAI API key:   export OPENAI_API_KEY=...")
		fmt.Fprintln(os.Stderr, "  - Anthropic API key: export ANTHROPIC_API_KEY=...")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd, err := backend.Generate(ctx, instruction)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dai: %s: %v\n", backend.Name(), err)
		os.Exit(1)
	}

	cmd = cleanCommand(cmd)

	if strings.HasPrefix(cmd, "ERROR:") {
		fmt.Fprintf(os.Stderr, "dai: could not generate command: %s\n", strings.TrimPrefix(cmd, "ERROR: "))
		os.Exit(1)
	}

	fmt.Printf("\033[1m> %s\033[0m\n", cmd)

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}

	c := exec.Command(shell, "-c", cmd)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	if err := c.Run(); err != nil {
		os.Exit(1)
	}
}

func cleanCommand(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "```bash")
	s = strings.TrimPrefix(s, "```sh")
	s = strings.TrimPrefix(s, "```shell")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
