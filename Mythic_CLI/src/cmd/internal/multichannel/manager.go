package multichannel

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var (
	ErrNoHealthyTransport = errors.New("no healthy transport available")
	ErrNoTaskAvailable    = errors.New("no task available")
)

type ManagerConfig struct {
	FailureThreshold int
	Cooldown         time.Duration
	BlobThreshold    int
}

type transportState struct {
	transport       Transport
	consecutiveFail int
	cooldownUntil   time.Time
	cursor          string
}

type CommunicationManager struct {
	agentID string
	store   LocalTaskStore
	cfg     ManagerConfig
	order   []*transportState
}

func NewCommunicationManager(agentID string, store LocalTaskStore, cfg ManagerConfig, transports ...Transport) *CommunicationManager {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = 3
	}
	if cfg.Cooldown <= 0 {
		cfg.Cooldown = 30 * time.Second
	}
	if cfg.BlobThreshold <= 0 {
		cfg.BlobThreshold = 256 * 1024
	}

	states := make([]*transportState, 0, len(transports))
	for _, t := range transports {
		states = append(states, &transportState{transport: t})
	}

	return &CommunicationManager{agentID: agentID, store: store, cfg: cfg, order: states}
}

func (m *CommunicationManager) Register(ctx context.Context, metadata map[string]string) error {
	for _, state := range m.order {
		if !state.available(time.Now()) {
			continue
		}
		if err := state.transport.Register(ctx, m.agentID, metadata); err == nil {
			state.resetFailures()
			return nil
		} else {
			state.markFailure(m.cfg)
		}
	}
	return ErrNoHealthyTransport
}

func (m *CommunicationManager) PullNextTask(ctx context.Context) (Task, Transport, error) {
	now := time.Now()
	for _, state := range m.order {
		if !state.available(now) {
			continue
		}
		health, err := state.transport.Healthcheck(ctx)
		if err != nil || health.State == HealthDown {
			state.markFailure(m.cfg)
			continue
		}

		tasks, cursor, err := state.transport.PullTasks(ctx, m.agentID, state.cursor)
		if err != nil {
			state.markFailure(m.cfg)
			continue
		}
		state.cursor = cursor
		state.resetFailures()
		if len(tasks) == 0 {
			continue
		}
		m.store.Save(tasks[0])
		return tasks[0], state.transport, nil
	}
	return Task{}, nil, ErrNoTaskAvailable
}

func (m *CommunicationManager) PushResult(ctx context.Context, transport Transport, task Task, resultPayload []byte, metadata map[string]string) error {
	result := Result{TaskID: task.ID, Payload: resultPayload, Metadata: metadata, FinishedAt: time.Now()}
	if len(resultPayload) > m.cfg.BlobThreshold {
		ref, err := transport.UploadBlob(ctx, resultPayload, metadata)
		if err != nil {
			return fmt.Errorf("upload blob: %w", err)
		}
		result.Payload = nil
		result.BlobRef = ref.Reference
	}
	if err := transport.PushResult(ctx, task.ID, result); err != nil {
		return fmt.Errorf("push result: %w", err)
	}
	if err := transport.Ack(ctx, task.ID); err != nil {
		return fmt.Errorf("ack task: %w", err)
	}
	m.store.Delete(task.ID)
	return nil
}

func (s *transportState) available(now time.Time) bool {
	return s.cooldownUntil.IsZero() || now.After(s.cooldownUntil)
}

func (s *transportState) markFailure(cfg ManagerConfig) {
	s.consecutiveFail++
	if s.consecutiveFail >= cfg.FailureThreshold {
		s.cooldownUntil = time.Now().Add(cfg.Cooldown)
	}
}

func (s *transportState) resetFailures() {
	s.consecutiveFail = 0
	s.cooldownUntil = time.Time{}
}
