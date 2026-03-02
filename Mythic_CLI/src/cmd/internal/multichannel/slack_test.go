package multichannel

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/slack-go/slack"
)

type fakeSlackClient struct {
	history *slack.GetConversationHistoryResponse
	posted  int
}

func (f *fakeSlackClient) AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error) {
	return &slack.AuthTestResponse{URL: "ok"}, nil
}
func (f *fakeSlackClient) PostMessageContext(ctx context.Context, channelID string, options ...slack.MsgOption) (string, string, error) {
	f.posted++
	return channelID, fmt.Sprintf("%d", time.Now().Unix()), nil
}
func (f *fakeSlackClient) GetConversationHistoryContext(ctx context.Context, params *slack.GetConversationHistoryParameters) (*slack.GetConversationHistoryResponse, error) {
	return f.history, nil
}
func (f *fakeSlackClient) AddReactionContext(ctx context.Context, name string, item slack.ItemRef) error {
	return nil
}
func (f *fakeSlackClient) UploadFileV2Context(ctx context.Context, params slack.UploadFileV2Parameters) (*slack.FileSummary, error) {
	return &slack.FileSummary{ID: "F123", Title: "blob"}, nil
}
func (f *fakeSlackClient) GetFileInfoContext(ctx context.Context, fileID string, count, page int) (*slack.File, []slack.Comment, *slack.Paging, error) {
	return &slack.File{ID: fileID, URLPrivateDownload: "https://files.local"}, nil, nil, nil
}

func TestSlackHealthcheckAndPull(t *testing.T) {
	taskPayload := base64.StdEncoding.EncodeToString([]byte("hello"))
	f := &fakeSlackClient{history: &slack.GetConversationHistoryResponse{Messages: []slack.Message{{Msg: slack.Msg{Text: `task:{"id":"t1","payload":"` + taskPayload + `","metadata":{"k":"v"}}`, Timestamp: "111.2"}}}}}
	f.history.ResponseMetaData.NextCursor = "next"
	s := &SlackTransport{client: f, channelID: "C123", clock: time.Now}

	st, err := s.Healthcheck(context.Background())
	if err != nil || st.State != HealthOK {
		t.Fatalf("unexpected health response: %+v, err=%v", st, err)
	}

	tasks, cursor, err := s.PullTasks(context.Background(), "agent", "")
	if err != nil {
		t.Fatalf("pull tasks failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "t1" || string(tasks[0].Payload) != "hello" || cursor != "next" {
		t.Fatalf("unexpected tasks result: %+v cursor=%s", tasks, cursor)
	}
}
