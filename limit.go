package limit

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
)

type IndexPool struct {
	mu   sync.Mutex
	pool []uint32
	next uint32
}

func (p *IndexPool) Get() uint32 {
	p.mu.Lock()
	end := len(p.pool)
	if end > 0 {
		end--
		index := p.pool[end]
		p.pool = p.pool[:end]
		p.mu.Unlock()
		return index
	}
	p.mu.Unlock()

	return atomic.AddUint32(&p.next, 1) - 1
}

func (p *IndexPool) Put(index ...uint32) {
	p.mu.Lock()
	p.pool = append(p.pool, index...)
	p.mu.Unlock()
}

type Janitor struct {
	period uint32
	idle   uint32
	last   uint32
	run    uint32
	one    uint32
}

type Pool interface {
	Get() uint32
	Put(index ...uint32)
}

type Limit[T comparable] struct {
	index    *xsync.MapOf[T, uint32]
	pool     Pool
	count    uint32
	period   uint32
	shards   atomic.Pointer[[]atomic.Pointer[[]uint32]]
	mu       sync.Mutex
	recycled []uint32
	config   Config
}

const (
	shardSize  = 1024
	expandStep = 1024
)

type Config struct{ Janitor }

type Option interface{ apply(*Config) }

type OptionFunc func(c *Config)

func (f OptionFunc) apply(c *Config) { f(c) }

func WithJanitor(period, idle uint32) Option {
	return OptionFunc(func(c *Config) { c.idle, c.period = idle, period })
}

func New[T comparable](count uint32, period uint32, options ...Option) *Limit[T] {
	var config Config
	for _, option := range options {
		option.apply(&config)
	}
	if config.period > 0 && config.period < period {
		config.period = period
	}
	if config.idle > 0 && config.idle < period {
		config.idle = period << 1
	}
	return &Limit[T]{
		count:  count,
		period: period,
		index:  xsync.NewMapOf[T, uint32](),
		pool:   &IndexPool{pool: make([]uint32, 0, expandStep)},
		config: config,
	}
}

func (l *Limit[T]) Index(id T) uint32 {
	index, _ := l.index.LoadOrCompute(id, func() uint32 {
		return l.pool.Get()
	})
	return index
}

// Allow return true if allowed
func (l *Limit[T]) Allow(id T) bool {
	if l.count == 0 {
		return false
	}
	if l.period == 0 {
		return true
	}

	t := time.Now()
	now := uint32(t.Unix())

	index := l.Index(id)
	shard := l.FindOrCreate(index)
	ring := shard[1:]
	pos := atomic.AddUint32(&shard[0], 1)
	end := atomic.SwapUint32(&ring[pos%l.count], now)

	if l.config.period > 0 && now-atomic.LoadUint32(&l.config.last) > l.config.period {
		if atomic.CompareAndSwapUint32(&l.config.run, 0, 1) {
			atomic.StoreUint32(&l.config.last, now)
			go l.Janitor(now)
		}
	}

	return now-end > l.period
}

func (l *Limit[T]) FindOrCreate(index uint32) []uint32 {
	bucket := index / shardSize
	offset := index % shardSize
	stride := l.count + 1
	begin := offset * stride

	shards := l.shards.Load()
	if shards != nil && bucket < uint32(len(*shards)) {
		shard := (*shards)[bucket].Load()
		if shard != nil {
			return (*shard)[begin : begin+stride]
		}
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	shards = l.shards.Load()
	if shards == nil || bucket >= uint32(len(*shards)) {
		newSize := ((bucket / expandStep) + 1) * expandStep
		newShards := make([]atomic.Pointer[[]uint32], newSize)
		if shards != nil {
			copy(newShards, *shards)
		}
		shards = &newShards
		l.shards.Store(shards)
	}

	shard := (*shards)[bucket].Load()
	if shard == nil {
		newShard := make([]uint32, shardSize*stride)
		shard = &newShard
		(*shards)[bucket].Store(shard)
	}

	return (*shard)[begin : begin+stride]
}

func (l *Limit[T]) Janitor(now uint32) {
	if !atomic.CompareAndSwapUint32(&l.config.one, 0, 1) {
		return
	}

	l.pool.Put(l.recycled...)
	l.recycled = l.recycled[:0]

	l.index.Range(func(id T, index uint32) bool {
		shard := l.FindOrCreate(index)
		pos := atomic.LoadUint32(&shard[0])
		if pos != 0 {
			end := atomic.LoadUint32(&shard[((pos-1)%l.count)+1])
			if now-end > l.config.idle {
				l.index.Delete(id)
				for i := range shard {
					atomic.StoreUint32(&shard[i], 0)
				}
				l.recycled = append(l.recycled, index)
			}
		}
		return true
	})

	atomic.StoreUint32(&l.config.one, 0)
	atomic.StoreUint32(&l.config.run, 0)
}

func (l *Limit[T]) Size() int {
	return l.index.Size()
}
