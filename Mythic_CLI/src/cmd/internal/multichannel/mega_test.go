package multichannel

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	mega "github.com/t3rm1n4l/go-mega"
)

type fakeMegaClient struct {
	storage map[string][]byte
}

func (f *fakeMegaClient) Login(email string, passwd string) error { return nil }
func (f *fakeMegaClient) FSRoot() *mega.Node                      { return &mega.Node{} }
func (f *fakeMegaClient) PathLookup(root *mega.Node, ns []string) ([]*mega.Node, error) {
	return []*mega.Node{{}}, nil
}
func (f *fakeMegaClient) CreateDir(name string, parent *mega.Node) (*mega.Node, error) {
	return &mega.Node{}, nil
}
func (f *fakeMegaClient) GetChildren(n *mega.Node) ([]*mega.Node, error) { return nil, nil }
func (f *fakeMegaClient) UploadFile(srcpath string, parent *mega.Node, name string, progress *chan int) (*mega.Node, error) {
	return &mega.Node{}, nil
}
func (f *fakeMegaClient) DownloadFile(src *mega.Node, dstpath string, progress *chan int) error {
	return errors.New("not used")
}
func (f *fakeMegaClient) Delete(node *mega.Node, destroy bool) error { return nil }
func (f *fakeMegaClient) GetUser() (mega.UserResp, error)            { return mega.UserResp{}, nil }

func TestMegaHealthcheck(t *testing.T) {
	m := &MegaTransport{client: &fakeMegaClient{}, basePath: "mythic", agentID: "a1"}
	status, err := m.Healthcheck(context.Background())
	if err != nil {
		t.Fatalf("healthcheck should not fail: %v", err)
	}
	if status.State != HealthOK {
		t.Fatalf("expected health ok, got %s", status.State)
	}
}

func TestMegaAckMissingTaskIsIdempotent(t *testing.T) {
	m := &MegaTransport{client: &fakeMegaClient{}, basePath: "base", agentID: "agent"}
	if err := m.Ack(context.Background(), "task-id"); err != nil {
		t.Fatalf("ack should be idempotent: %v", err)
	}
	if !strings.Contains(path.Join("base", "agent", "tasks", "task-id.json"), "task-id.json") {
		t.Fatal("sanity check failed")
	}
}
