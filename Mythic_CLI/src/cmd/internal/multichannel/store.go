package multichannel

import "sync"

// LocalTaskStore provides durable-like queue semantics for in-flight tasks.
// This MVP implementation keeps state in memory and can be replaced by SQLite/file-backed storage.
type LocalTaskStore interface {
	Save(Task)
	Delete(taskID string)
	List() []Task
}

// MemoryTaskStore is a thread-safe in-memory LocalTaskStore implementation.
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]Task
}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{tasks: make(map[string]Task)}
}

func (s *MemoryTaskStore) Save(task Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
}

func (s *MemoryTaskStore) Delete(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, taskID)
}

func (s *MemoryTaskStore) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		result = append(result, task)
	}
	return result
}
