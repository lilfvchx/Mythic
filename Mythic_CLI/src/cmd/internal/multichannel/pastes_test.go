package multichannel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPastesHealthAndRoundTrip(t *testing.T) {
	var lastPaste string
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/documents":
			lastPaste = ts.URL + "/p/abc"
			w.Header().Set("Location", lastPaste)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/p/abc":
			body, _ := json.Marshal(map[string]any{"agent_id": "agent-1", "id": "task-1", "payload": base64.StdEncoding.EncodeToString([]byte("run")), "metadata": map[string]string{"k": "v"}})
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	p, err := NewPastesTransport(ts.URL)
	if err != nil {
		t.Fatalf("new transport failed: %v", err)
	}
	if err := p.Register(context.Background(), "agent-1", nil); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	health, err := p.Healthcheck(context.Background())
	if err != nil || health.State != HealthOK {
		t.Fatalf("bad health: %+v err=%v", health, err)
	}
	if err := p.PushResult(context.Background(), "task-1", Result{TaskID: "task-1", Payload: []byte("ok"), FinishedAt: time.Now()}); err != nil {
		t.Fatalf("push result failed: %v", err)
	}
	tasks, next, err := p.PullTasks(context.Background(), "agent-1", lastPaste)
	if err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" || string(tasks[0].Payload) != "run" || next != "" {
		t.Fatalf("unexpected tasks: %+v next=%q", tasks, next)
	}
}
