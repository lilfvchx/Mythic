package multichannel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
)

const slackTaskPrefix = "task:"

// SlackTransport uses Slack channels for control and file APIs for blob transfer.
type SlackTransport struct {
	client    slackClient
	channelID string
	clock     func() time.Time
	mu        sync.Mutex
}

type slackClient interface {
	AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error)
	PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error)
	GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error)
	AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error
	UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error)
	GetFileInfoContext(ctx context.Context, fileID string, count, page int) (*slack.File, []slack.Comment, *slack.Paging, error)
}

func NewSlackTransport(token, channelID string) (*SlackTransport, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("slack token is required")
	}
	if strings.TrimSpace(channelID) == "" {
		return nil, errors.New("slack channel id is required")
	}
	return &SlackTransport{client: slack.New(token), channelID: channelID, clock: time.Now}, nil
}

func (s *SlackTransport) Name() string { return "slack" }

func (s *SlackTransport) Register(ctx context.Context, agentID string, metadata map[string]string) error {
	body, err := json.Marshal(map[string]any{"event": "register", "agent_id": agentID, "metadata": metadata, "ts": s.clock().UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return fmt.Errorf("marshal register payload: %w", err)
	}
	return s.withBackoff(ctx, func() error {
		_, _, err := s.client.PostMessageContext(ctx, s.channelID, slack.MsgOptionText(string(body), false))
		return err
	})
}

func (s *SlackTransport) PullTasks(ctx context.Context, agentID, cursor string) ([]Task, string, error) {
	params := &slack.GetConversationHistoryParameters{ChannelID: s.channelID, Cursor: cursor, Limit: 50}
	resp, err := s.client.GetConversationHistoryContext(ctx, params)
	if err != nil {
		return nil, cursor, fmt.Errorf("slack history: %w", err)
	}
	tasks := make([]Task, 0)
	for _, msg := range resp.Messages {
		if !strings.HasPrefix(msg.Msg.Text, slackTaskPrefix) {
			continue
		}
		task, err := decodeSlackTask(strings.TrimPrefix(msg.Msg.Text, slackTaskPrefix))
		if err != nil {
			continue
		}
		if task.ID == "" {
			task.ID = msg.Msg.Timestamp
		}
		tasks = append(tasks, task)
	}
	return tasks, resp.ResponseMetaData.NextCursor, nil
}

func (s *SlackTransport) PushResult(ctx context.Context, taskID string, result Result) error {
	payload := map[string]any{"event": "result", "task_id": taskID, "payload": base64.StdEncoding.EncodeToString(result.Payload), "blob_ref": result.BlobRef, "metadata": result.Metadata, "finished_at": result.FinishedAt.UTC().Format(time.RFC3339Nano)}
	blob, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return s.withBackoff(ctx, func() error {
		_, _, err := s.client.PostMessageContext(ctx, s.channelID, slack.MsgOptionText(string(blob), false), slack.MsgOptionTS(taskID))
		return err
	})
}

func (s *SlackTransport) UploadBlob(ctx context.Context, bytes []byte, metadata map[string]string) (BlobRef, error) {
	params := slack.UploadFileV2Parameters{Channel: s.channelID, Filename: "blob.bin", Reader: strings.NewReader(base64.StdEncoding.EncodeToString(bytes)), FileSize: len(base64.StdEncoding.EncodeToString(bytes))}
	if v := metadata["filename"]; v != "" {
		params.Filename = v
	}
	if v := metadata["title"]; v != "" {
		params.Title = v
	}
	var out *slack.FileSummary
	if err := s.withBackoff(ctx, func() error {
		var err error
		out, err = s.client.UploadFileV2Context(ctx, params)
		return err
	}); err != nil {
		return BlobRef{}, fmt.Errorf("upload blob to slack: %w", err)
	}
	return BlobRef{Reference: out.ID, Metadata: map[string]string{"provider": "slack"}}, nil
}

func (s *SlackTransport) DownloadBlob(ctx context.Context, ref BlobRef) ([]byte, error) {
	file, _, _, err := s.client.GetFileInfoContext(ctx, ref.Reference, 1, 1)
	if err != nil {
		return nil, fmt.Errorf("slack get file: %w", err)
	}
	if file.URLPrivateDownload == "" {
		return nil, errors.New("slack file has no private download URL")
	}
	return nil, errors.New("slack blob download requires authenticated HTTP fetch from URLPrivateDownload")
}

func (s *SlackTransport) Ack(ctx context.Context, taskID string) error {
	ts := taskID
	if _, err := strconv.ParseFloat(ts, 64); err != nil {
		return fmt.Errorf("slack ack taskID must be timestamp: %w", err)
	}
	return s.withBackoff(ctx, func() error {
		return s.client.AddReactionContext(ctx, "white_check_mark", slack.NewRefToMessage(s.channelID, ts))
	})
}

func (s *SlackTransport) Healthcheck(ctx context.Context) (Status, error) {
	_, err := s.client.AuthTestContext(ctx)
	if err != nil {
		return Status{State: HealthDown, Message: err.Error()}, err
	}
	return Status{State: HealthOK, Message: "slack API reachable"}, nil
}

func (s *SlackTransport) withBackoff(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 4; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		wait := time.Duration(1<<attempt) * 100 * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return err
}

func decodeSlackTask(encoded string) (Task, error) {
	var payload struct {
		ID       string            `json:"id"`
		Payload  string            `json:"payload"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(encoded), &payload); err != nil {
		return Task{}, err
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Payload)
	if err != nil {
		return Task{}, err
	}
	return Task{ID: payload.ID, Payload: decoded, Metadata: payload.Metadata, CreatedAt: time.Now().UTC()}, nil
}
