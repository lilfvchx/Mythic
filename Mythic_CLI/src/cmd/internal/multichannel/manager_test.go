package multichannel

import (
	"context"
	"errors"
	"testing"
)

type mockTransport struct {
	name          string
	health        Status
	pullTasks     []Task
	pullErr       error
	registerErr   error
	pushErr       error
	ackErr        error
	uploadErr     error
	uploadCalled  bool
	pushCalled    bool
	ackCalled     bool
}

func (m *mockTransport) Name() string { return m.name }
func (m *mockTransport) Register(_ context.Context, _ string, _ map[string]string) error {
	return m.registerErr
}
func (m *mockTransport) PullTasks(_ context.Context, _, _ string) ([]Task, string, error) {
	return m.pullTasks, "cursor", m.pullErr
}
func (m *mockTransport) PushResult(_ context.Context, _ string, _ Result) error {
	m.pushCalled = true
	return m.pushErr
}
func (m *mockTransport) UploadBlob(_ context.Context, _ []byte, _ map[string]string) (BlobRef, error) {
	m.uploadCalled = true
	if m.uploadErr != nil {
		return BlobRef{}, m.uploadErr
	}
	return BlobRef{Reference: "blob://mock"}, nil
}
func (m *mockTransport) DownloadBlob(_ context.Context, _ BlobRef) ([]byte, error) { return nil, nil }
func (m *mockTransport) Ack(_ context.Context, _ string) error {
	m.ackCalled = true
	return m.ackErr
}
func (m *mockTransport) Healthcheck(_ context.Context) (Status, error) { return m.health, nil }

func TestPullNextTaskFailover(t *testing.T) {
	primary := &mockTransport{name: "slack", health: Status{State: HealthDown}}
	backup := &mockTransport{name: "github", health: Status{State: HealthOK}, pullTasks: []Task{{ID: "t1"}}}
	mgr := NewCommunicationManager("agent-1", NewMemoryTaskStore(), ManagerConfig{FailureThreshold: 1}, primary, backup)

	task, transport, err := mgr.PullNextTask(context.Background())
	if err != nil {
		t.Fatalf("expected task, got error: %v", err)
	}
	if task.ID != "t1" {
		t.Fatalf("unexpected task id: %s", task.ID)
	}
	if transport.Name() != "github" {
		t.Fatalf("expected github transport, got %s", transport.Name())
	}
}

func TestPushResultUploadsLargePayload(t *testing.T) {
	transport := &mockTransport{name: "dropbox", health: Status{State: HealthOK}}
	mgr := NewCommunicationManager("agent-1", NewMemoryTaskStore(), ManagerConfig{BlobThreshold: 4}, transport)
	task := Task{ID: "t2"}
	mgr.store.Save(task)

	if err := mgr.PushResult(context.Background(), transport, task, []byte("payload-too-large"), map[string]string{"kind": "stdout"}); err != nil {
		t.Fatalf("push result failed: %v", err)
	}
	if !transport.uploadCalled {
		t.Fatal("expected UploadBlob to be called")
	}
	if !transport.pushCalled || !transport.ackCalled {
		t.Fatal("expected PushResult and Ack to be called")
	}
}

func TestPushResultPropagatesUploadFailure(t *testing.T) {
	transport := &mockTransport{name: "mega", health: Status{State: HealthOK}, uploadErr: errors.New("upload failed")}
	mgr := NewCommunicationManager("agent-1", NewMemoryTaskStore(), ManagerConfig{BlobThreshold: 1}, transport)
	err := mgr.PushResult(context.Background(), transport, Task{ID: "t3"}, []byte("abc"), nil)
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}
