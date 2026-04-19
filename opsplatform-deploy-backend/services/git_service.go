package services

import (
	"sync"
)

type LockMgr struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func NewLockMgr() *LockMgr {
	return &LockMgr{locks: make(map[string]*sync.Mutex)}
}

func (m *LockMgr) get(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}

func (m *LockMgr) Acquire(key string) { m.get(key).Lock() }
func (m *LockMgr) Release(key string) { m.get(key).Unlock() }
