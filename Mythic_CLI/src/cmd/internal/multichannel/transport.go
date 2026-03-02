package multichannel

import "context"

// Transport defines a common contract that enables channel failover.
type Transport interface {
	Name() string
	Register(ctx context.Context, agentID string, metadata map[string]string) error
	PullTasks(ctx context.Context, agentID, cursor string) ([]Task, string, error)
	PushResult(ctx context.Context, taskID string, result Result) error
	UploadBlob(ctx context.Context, bytes []byte, metadata map[string]string) (BlobRef, error)
	DownloadBlob(ctx context.Context, ref BlobRef) ([]byte, error)
	Ack(ctx context.Context, taskID string) error
	Healthcheck(ctx context.Context) (Status, error)
}
