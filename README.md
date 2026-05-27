# dai

AI-powered shell command generator. Describe what you want in plain English, get the command and execute it.

```
dai "create a merge request for this branch"
```

## Install

```sh
go install github.com/xy3/dai@latest
```

Requires Go 1.21+.

## Usage

```sh
dai <instruction>
```

Examples:

```sh
dai "find all Go files modified in the last week"
dai "compress this directory into a tar.gz excluding node_modules"
dai "show me the git log for the current branch since it diverged from main"
dai "kill the process running on port 3000"
```

dai prints the generated command in bold, then executes it.

## Backends

dai auto-detects an available AI backend. First one found wins:

| # | Backend | Setup |
|---|---------|-------|
| 1 | Gemini CLI (OAuth) | `npm install -g @google/gemini-cli` and authenticate |
| 2 | Gemini CLI (fallback) | Same as above (slower, used if OAuth direct-API fails) |
| 3 | Gemini (gcloud) | `gcloud auth application-default login` |
| 4 | Gemini API key | `export GEMINI_API_KEY=...` |
| 5 | OpenAI | `export OPENAI_API_KEY=...` |
| 6 | Anthropic | `export ANTHROPIC_API_KEY=...` |

The Gemini CLI backend extracts your existing OAuth credentials from `~/.gemini/oauth_creds.json` and calls the API directly — no API key needed, ~1s response time. If you have a Gemini Code Assist subscription through your company and the CLI installed, dai works out of the box.

## How it works

dai sends your instruction to the AI with a system prompt that constrains the output to a single raw shell command. It strips markdown formatting, prints the command, and runs it in your shell.
