package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"context-steward/internal/config"
)

type Client struct {
	cfg *config.Config
	cli *http.Client
}

type GenerateRequest struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Format  string                 `json:"format,omitempty"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type GenerateResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
}

type DecisionExtraction struct {
	Title        string `json:"title"`
	DecisionText string `json:"decision"`
	Rationale    string `json:"rationale"`
	Consequences string `json:"consequences"`
}

type HandoffExtraction struct {
	Decisions     []DecisionExtraction `json:"decisions"`
	OpenQuestions []string             `json:"open_questions"`
}

// NewClient instantiates a new Ollama HTTP client
func NewClient(cfg *config.Config) *Client {
	return &Client{
		cfg: cfg,
		cli: &http.Client{
			Timeout: 2 * time.Minute, // LLM requests can be slow depending on hardware
		},
	}
}

func (c *Client) callOllama(prompt string, requireJSON bool) (string, error) {
	if !c.cfg.LLM.Enabled {
		return "LLM integration is disabled in config.", nil
	}

	url := fmt.Sprintf("%s/api/generate", c.cfg.LLM.Endpoint)
	reqBody := GenerateRequest{
		Model:  c.cfg.LLM.Model,
		Prompt: prompt,
		Stream: false,
		Options: map[string]interface{}{
			"temperature": 0.0, // Keep generation deterministic
		},
	}
	if requireJSON {
		reqBody.Format = "json"
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama server connection failed at %s. Please ensure Ollama is running: %w", c.cfg.LLM.Endpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama server returned HTTP error: %s", resp.Status)
	}

	var genResp GenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&genResp); err != nil {
		return "", fmt.Errorf("failed to decode Ollama response: %w", err)
	}

	return genResp.Response, nil
}

// SummarizeFile calls the local LLM to generate a dense, concise summary of a file's content
func (c *Client) SummarizeFile(content string) (string, error) {
	prompt := fmt.Sprintf(`You are Context Steward, an AI workspace context manager.
Your task is to provide a dense, concise summary of the following document.
Focus on:
1. The core purpose of the file.
2. Major components, types, functions, or concepts defined.
3. Crucial architectural rules, requirements, or decisions it documents.

Keep the summary brief and dense (usually under 200 words). Do not include any intro, outro, or meta-commentary.

Document content:
---
%s
---
Summary:`, content)

	return c.callOllama(prompt, false)
}

// ExtractHandoff calls the local LLM using JSON format mode to extract decisions and questions from unstructured markdown text
func (c *Client) ExtractHandoff(content string) (HandoffExtraction, error) {
	prompt := fmt.Sprintf(`You are Context Steward, an AI workspace context manager.
Ingest the following session notes/handoff markdown and extract any:
1. Durable decisions made (including title, the decision, rationale, and consequences).
2. Key open questions that remain unanswered.

You MUST return a JSON object strictly matching this schema:
{
  "decisions": [
    {
      "title": "Short title of the decision",
      "decision": "Details of what was decided",
      "rationale": "Why this decision was made",
      "consequences": "What are the effects or next steps of this decision"
    }
  ],
  "open_questions": [
    "Question 1 description",
    "Question 2 description"
  ]
}

If no decisions or questions are found, return empty lists. Do not include any extra text outside the JSON.

Session notes / handoff:
---
%s
---
JSON:`, content)

	response, err := c.callOllama(prompt, true)
	if err != nil {
		return HandoffExtraction{}, err
	}

	var ext HandoffExtraction
	if err := json.Unmarshal([]byte(response), &ext); err != nil {
		return HandoffExtraction{}, fmt.Errorf("failed to parse JSON response from LLM: %w. Response was: %s", err, response)
	}

	return ext, nil
}
