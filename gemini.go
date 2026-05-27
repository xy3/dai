package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const geminiProjectCacheFile = ".dai_project"

// GeminiCLIBackend uses the locally installed gemini CLI. No API key needed.
// It first tries a fast direct-API path using the CLI's OAuth credentials
// via the cloudcode-pa.googleapis.com endpoint. Falls back to the slower
// gemini CLI process if that fails.
type GeminiCLIBackend struct{}

func (b *GeminiCLIBackend) Name() string { return "gemini" }

func (b *GeminiCLIBackend) Available() bool {
	_, err := exec.LookPath("gemini")
	return err == nil
}

func (b *GeminiCLIBackend) credsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

func (b *GeminiCLIBackend) projectCachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", geminiProjectCacheFile)
}

func (b *GeminiCLIBackend) getAccessToken(ctx context.Context) (string, error) {
	data, err := os.ReadFile(b.credsPath())
	if err != nil {
		return "", err
	}

	var creds struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiryDate   int64  `json:"expiry_date"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", err
	}

	if time.Now().UnixMilli() < creds.ExpiryDate-60000 {
		return creds.AccessToken, nil
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"client_id":     {"32555940559.apps.googleusercontent.com"},
		"refresh_token": {creds.RefreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var refreshed struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &refreshed) != nil || refreshed.AccessToken == "" {
		return "", fmt.Errorf("token refresh failed: %s", string(body))
	}

	creds.AccessToken = refreshed.AccessToken
	creds.ExpiryDate = time.Now().UnixMilli() + refreshed.ExpiresIn*1000
	newData, _ := json.Marshal(creds)
	os.WriteFile(b.credsPath(), newData, 0600)

	return refreshed.AccessToken, nil
}

func (b *GeminiCLIBackend) getProjectID(ctx context.Context, accessToken string) (string, error) {
	if cached, err := os.ReadFile(b.projectCachePath()); err == nil {
		return strings.TrimSpace(string(cached)), nil
	}

	reqBody := map[string]interface{}{
		"metadata": map[string]string{
			"ideType":    "IDE_UNSPECIFIED",
			"platform":   "PLATFORM_UNSPECIFIED",
			"pluginType": "GEMINI",
		},
	}
	jsonBody, _ := json.Marshal(reqBody)

	req, _ := http.NewRequestWithContext(ctx, "POST",
		"https://cloudcode-pa.googleapis.com/v1internal:loadCodeAssist",
		bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "GeminiCLI/1.0")
	req.Header.Set("x-activity-request-id", randomUUID())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var result struct {
		CloudaicompanionProject string `json:"cloudaicompanionProject"`
	}
	if json.Unmarshal(body, &result) != nil || result.CloudaicompanionProject == "" {
		var alt struct {
			CloudaicompanionProject struct {
				ID string `json:"id"`
			} `json:"cloudaicompanionProject"`
		}
		if json.Unmarshal(body, &alt) != nil || alt.CloudaicompanionProject.ID == "" {
			return "", fmt.Errorf("failed to get project ID: %s", string(body))
		}
		result.CloudaicompanionProject = alt.CloudaicompanionProject.ID
	}

	projectID := result.CloudaicompanionProject
	os.WriteFile(b.projectCachePath(), []byte(projectID), 0600)
	return projectID, nil
}

func (b *GeminiCLIBackend) tryOAuthFastPath(ctx context.Context, instruction string) (string, bool) {
	token, err := b.getAccessToken(ctx)
	if err != nil {
		return "", false
	}

	projectID, err := b.getProjectID(ctx, token)
	if err != nil {
		return "", false
	}

	result, err := callCloudCodeAPI(ctx, instruction, token, projectID)
	if err != nil {
		return "", false
	}
	return result, true
}

func (b *GeminiCLIBackend) Generate(ctx context.Context, instruction string) (string, error) {
	if result, ok := b.tryOAuthFastPath(ctx, instruction); ok {
		return result, nil
	}

	prompt := systemPrompt + "\n\nInstruction: " + instruction
	cmd := exec.CommandContext(ctx, "gemini", "-p", prompt)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gemini CLI failed: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

func callCloudCodeAPI(ctx context.Context, instruction, accessToken, projectID string) (string, error) {
	sessionID := randomUUID()
	promptID := randomUUID()

	body := map[string]interface{}{
		"project":        projectID,
		"model":          "gemini-2.5-flash",
		"user_prompt_id": promptID,
		"request": map[string]interface{}{
			"session_id": sessionID,
			"systemInstruction": map[string]interface{}{
				"parts": []map[string]string{{"text": systemPrompt}},
			},
			"contents": []map[string]interface{}{
				{"role": "user", "parts": []map[string]string{{"text": instruction}}},
			},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://cloudcode-pa.googleapis.com/v1internal:generateContent",
		bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "GeminiCLI/1.0")
	req.Header.Set("x-activity-request-id", randomUUID())

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Cloud Code API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Response struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		} `json:"response"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Response.Candidates) == 0 || len(result.Response.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response")
	}

	return result.Response.Candidates[0].Content.Parts[0].Text, nil
}

func randomUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// GeminiGcloudBackend uses gcloud application default credentials.
type GeminiGcloudBackend struct{}

func (b *GeminiGcloudBackend) Name() string { return "gemini (gcloud)" }

func (b *GeminiGcloudBackend) Available() bool {
	if _, err := exec.LookPath("gcloud"); err != nil {
		return false
	}
	credsFile := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credsFile == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		credsFile = filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	}
	_, err := os.Stat(credsFile)
	return err == nil
}

func (b *GeminiGcloudBackend) Generate(ctx context.Context, instruction string) (string, error) {
	tokenCmd := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "print-access-token")
	tokenOut, err := tokenCmd.Output()
	if err != nil {
		return "", fmt.Errorf("gcloud auth failed: %w", err)
	}
	return callGeminiAPI(ctx, instruction, "", strings.TrimSpace(string(tokenOut)))
}

// GeminiAPIBackend uses the Gemini API with GEMINI_API_KEY.
type GeminiAPIBackend struct{}

func (b *GeminiAPIBackend) Name() string { return "gemini-api" }

func (b *GeminiAPIBackend) Available() bool {
	return os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_API_KEY") != ""
}

func (b *GeminiAPIBackend) apiKey() string {
	if k := os.Getenv("GEMINI_API_KEY"); k != "" {
		return k
	}
	return os.Getenv("GOOGLE_API_KEY")
}

func (b *GeminiAPIBackend) Generate(ctx context.Context, instruction string) (string, error) {
	return callGeminiAPI(ctx, instruction, b.apiKey(), "")
}

func callGeminiAPI(ctx context.Context, instruction, apiKey, bearerToken string) (string, error) {
	body := map[string]interface{}{
		"system_instruction": map[string]interface{}{
			"parts": []map[string]string{{"text": systemPrompt}},
		},
		"contents": []map[string]interface{}{
			{
				"role": "user",
				"parts": []map[string]string{{"text": instruction}},
			},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	urlStr := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent"
	if apiKey != "" {
		urlStr += "?key=" + apiKey
	}

	req, err := http.NewRequestWithContext(ctx, "POST", urlStr, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("Gemini API error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini returned empty response")
	}

	return result.Candidates[0].Content.Parts[0].Text, nil
}
