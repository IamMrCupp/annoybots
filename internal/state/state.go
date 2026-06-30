// Package state is F3: a small shared key/counter/leaderboard store. Backed by
// the botnet Redis so karma, awards, and (later) IdleRPG state are network-wide
// and shared across every bot — even bots on other hosts that share the bus.
// Falls back to an in-memory store when the botnet/Redis is disabled.
package state

import (
	"context"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Entry is one ranked member of a leaderboard.
type Entry struct {
	Member string
	Score  int64
}

// Store is the persistence surface features use. Counters are plain integers;
// leaderboards are sorted sets (ZIncr maintains the ranking as it increments).
type Store interface {
	Incr(ctx context.Context, key string, delta int64) (int64, error)
	Get(ctx context.Context, key string) (int64, error)
	ZIncr(ctx context.Context, key, member string, delta int64) (int64, error)
	ZScore(ctx context.Context, key, member string) (int64, error)
	ZTop(ctx context.Context, key string, n int) ([]Entry, error)
	ZRem(ctx context.Context, key, member string) error // remove a member from a sorted set
	// Hash ops — structured per-entity records (e.g. an IdleRPG player sheet).
	HIncr(ctx context.Context, key, field string, delta int64) (int64, error)
	HSet(ctx context.Context, key, field string, value int64) error
	HGetAll(ctx context.Context, key string) (map[string]int64, error)
	// String ops — small text values (identity→account links, password hashes).
	SetStr(ctx context.Context, key, value string) error
	GetStr(ctx context.Context, key string) (string, error)
	Del(ctx context.Context, key string) error
	// List ops — a capped, newest-first log (e.g. the IdleRPG activity feed).
	ListPush(ctx context.Context, key, value string, limit int) error // prepend, then trim to cap newest
	ListRange(ctx context.Context, key string, n int) ([]string, error)
	Close() error
}

const opTimeout = 3 * time.Second

// --- Redis-backed (shared across bots/hosts) ---

type redisStore struct {
	c      *redis.Client
	prefix string
}

// NewRedis returns a Store on its own Redis client (separate pool from the bus).
// prefix namespaces all keys (e.g. "annoybots:state:").
func NewRedis(addr, password, prefix string) Store {
	return &redisStore{
		c:      redis.NewClient(&redis.Options{Addr: addr, Password: password}),
		prefix: prefix,
	}
}

func (s *redisStore) k(key string) string { return s.prefix + key }

func (s *redisStore) ctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, opTimeout)
}

func (s *redisStore) Incr(ctx context.Context, key string, delta int64) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	return s.c.IncrBy(ctx, s.k(key), delta).Result()
}

func (s *redisStore) Get(ctx context.Context, key string) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	v, err := s.c.Get(ctx, s.k(key)).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return v, err
}

func (s *redisStore) ZIncr(ctx context.Context, key, member string, delta int64) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	f, err := s.c.ZIncrBy(ctx, s.k(key), float64(delta), member).Result()
	return int64(f), err
}

func (s *redisStore) ZScore(ctx context.Context, key, member string) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	f, err := s.c.ZScore(ctx, s.k(key), member).Result()
	if err == redis.Nil {
		return 0, nil
	}
	return int64(f), err
}

func (s *redisStore) ZTop(ctx context.Context, key string, n int) ([]Entry, error) {
	if n <= 0 {
		return nil, nil
	}
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	zs, err := s.c.ZRevRangeWithScores(ctx, s.k(key), 0, int64(n-1)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(zs))
	for _, z := range zs {
		m, _ := z.Member.(string)
		out = append(out, Entry{Member: m, Score: int64(z.Score)})
	}
	return out, nil
}

func (s *redisStore) ZRem(ctx context.Context, key, member string) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	return s.c.ZRem(ctx, s.k(key), member).Err()
}

func (s *redisStore) HIncr(ctx context.Context, key, field string, delta int64) (int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	return s.c.HIncrBy(ctx, s.k(key), field, delta).Result()
}

func (s *redisStore) HSet(ctx context.Context, key, field string, value int64) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	return s.c.HSet(ctx, s.k(key), field, value).Err()
}

func (s *redisStore) HGetAll(ctx context.Context, key string) (map[string]int64, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	raw, err := s.c.HGetAll(ctx, s.k(key)).Result()
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(raw))
	for f, v := range raw {
		n, _ := strconv.ParseInt(v, 10, 64)
		out[f] = n
	}
	return out, nil
}

func (s *redisStore) SetStr(ctx context.Context, key, value string) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	return s.c.Set(ctx, s.k(key), value, 0).Err()
}

func (s *redisStore) GetStr(ctx context.Context, key string) (string, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	v, err := s.c.Get(ctx, s.k(key)).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}

func (s *redisStore) Del(ctx context.Context, key string) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	return s.c.Del(ctx, s.k(key)).Err()
}

func (s *redisStore) ListPush(ctx context.Context, key, value string, limit int) error {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	pipe := s.c.TxPipeline()
	pipe.LPush(ctx, s.k(key), value)
	if limit > 0 {
		pipe.LTrim(ctx, s.k(key), 0, int64(limit-1))
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *redisStore) ListRange(ctx context.Context, key string, n int) ([]string, error) {
	ctx, cancel := s.ctx(ctx)
	defer cancel()
	if n <= 0 {
		return nil, nil
	}
	return s.c.LRange(ctx, s.k(key), 0, int64(n-1)).Result()
}

func (s *redisStore) Close() error { return s.c.Close() }

// --- In-memory fallback (single process; used when Redis is off, and in tests) ---

type memStore struct {
	mu       sync.Mutex
	counters map[string]int64
	zsets    map[string]map[string]int64
	hashes   map[string]map[string]int64
	strs     map[string]string
	lists    map[string][]string
}

// NewMem returns an in-process Store.
func NewMem() Store {
	return &memStore{
		counters: map[string]int64{},
		zsets:    map[string]map[string]int64{},
		hashes:   map[string]map[string]int64{},
		strs:     map[string]string{},
		lists:    map[string][]string{},
	}
}

func (m *memStore) Incr(_ context.Context, key string, delta int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[key] += delta
	return m.counters[key], nil
}

func (m *memStore) Get(_ context.Context, key string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[key], nil
}

func (m *memStore) ZIncr(_ context.Context, key, member string, delta int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.zsets[key]
	if z == nil {
		z = map[string]int64{}
		m.zsets[key] = z
	}
	z[member] += delta
	return z[member], nil
}

func (m *memStore) ZScore(_ context.Context, key, member string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.zsets[key][member], nil
}

func (m *memStore) ZTop(_ context.Context, key string, n int) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	z := m.zsets[key]
	out := make([]Entry, 0, len(z))
	for member, score := range z {
		out = append(out, Entry{Member: member, Score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Member < out[j].Member
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out, nil
}

func (m *memStore) ZRem(_ context.Context, key, member string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.zsets[key], member)
	return nil
}

func (m *memStore) HIncr(_ context.Context, key, field string, delta int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.hashes[key]
	if h == nil {
		h = map[string]int64{}
		m.hashes[key] = h
	}
	h[field] += delta
	return h[field], nil
}

func (m *memStore) HSet(_ context.Context, key, field string, value int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.hashes[key]
	if h == nil {
		h = map[string]int64{}
		m.hashes[key] = h
	}
	h[field] = value
	return nil
}

func (m *memStore) HGetAll(_ context.Context, key string) (map[string]int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]int64, len(m.hashes[key]))
	for f, v := range m.hashes[key] {
		out[f] = v
	}
	return out, nil
}

func (m *memStore) SetStr(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.strs[key] = value
	return nil
}

func (m *memStore) GetStr(_ context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.strs[key], nil
}

func (m *memStore) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.strs, key)
	delete(m.counters, key)
	delete(m.zsets, key)
	delete(m.hashes, key)
	delete(m.lists, key)
	return nil
}

func (m *memStore) ListPush(_ context.Context, key, value string, limit int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := append([]string{value}, m.lists[key]...) // newest first
	if limit > 0 && len(l) > limit {
		l = l[:limit]
	}
	m.lists[key] = l
	return nil
}

func (m *memStore) ListRange(_ context.Context, key string, n int) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.lists[key]
	if n > 0 && len(l) > n {
		l = l[:n]
	}
	out := make([]string, len(l))
	copy(out, l)
	return out, nil
}

func (m *memStore) Close() error { return nil }
