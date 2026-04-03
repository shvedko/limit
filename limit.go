package limit

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v3"
)

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

type Stats struct {
	threshold int
	recycled  uint64
}

type Config struct {
	penalty bool
	pool    Pool
	Janitor
	Stats
}

type Option interface{ apply(*Config) }

type OptionFunc func(c *Config)

func (f OptionFunc) apply(c *Config) { f(c) }

func WithJanitor(period, idle uint32) Option {
	return OptionFunc(func(c *Config) { c.idle, c.period = idle, period })
}

func WithJanitorThreshold(percentage float64) Option {
	return OptionFunc(func(c *Config) { c.threshold = int(min(1, max(0, percentage)) * 100) })
}

func WithPool(pool Pool) Option {
	return OptionFunc(func(c *Config) { c.pool = pool })
}

func WithPenalty() Option {
	return OptionFunc(func(c *Config) { c.penalty = true })
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
	if config.pool == nil {
		config.pool = NewPool()
	}
	config.last = uint32(time.Now().Unix())
	return &Limit[T]{
		count:  count,
		period: period,
		index:  xsync.NewMapOf[T, uint32](),
		config: config,
	}
}

func (l *Limit[T]) Index(id T) uint32 {
	index, _ := l.index.LoadOrCompute(id, func() uint32 {
		return l.config.pool.Get()
	})
	return index
}

type Shard []uint32

func (s Shard) AllowWithPenalty(now, count, period uint32) bool {
	r := s[1:]
	pos := atomic.AddUint32(&s[0], 1) % count
	end := atomic.SwapUint32(&r[pos], now)
	return now-end > period
}

func (s Shard) AllowWithoutPenalty(now uint32, count uint32, period uint32) bool {
	var miss uint64
	r := s[1:]
	for {
		pos := atomic.LoadUint32(&s[0])
		mod := pos % count
		end := atomic.LoadUint32(&r[mod])
		if now-end < period {
			return false
		}
		if atomic.CompareAndSwapUint32(&s[0], pos, pos+1) {
			atomic.StoreUint32(&r[mod], now)
			return true
		}
		miss++
		runtime.Gosched()
	}
}

func (s Shard) Allow(now uint32, count uint32, period uint32, penalty bool) bool {
	if penalty {
		return s.AllowWithPenalty(now, count, period)
	}
	return s.AllowWithoutPenalty(now, count, period)
}

func (l *Limit[T]) Allow(id T) bool {
	if l.count == 0 {
		return false
	}
	if l.period == 0 {
		return true
	}

	now := uint32(time.Now().Unix())

	index := l.Index(id)
	shard := l.FindOrCreate(index)
	allow := shard.Allow(now, l.count, l.period, l.config.penalty)
	if l.config.period > 0 && now-atomic.LoadUint32(&l.config.last) > l.config.period {
		if atomic.CompareAndSwapUint32(&l.config.run, 0, 1) {
			atomic.StoreUint32(&l.config.last, now)
			go l.Janitor(now)
		}
	}

	return allow
}

func (l *Limit[T]) FindOrCreate(index uint32) Shard {
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

	size := len(l.recycled)
	part := size / ringSize * ringSize
	from := size - part
	if part >= size*l.config.threshold/100 {
		l.config.pool.Put(l.recycled[from:]...)
		l.recycled = l.recycled[:from]
	}

	l.index.Range(func(id T, index uint32) bool {
		shard := l.FindOrCreate(index)
		pos := atomic.LoadUint32(&shard[0])
		if pos != 0 {
			end := atomic.LoadUint32(&shard[((pos-1)%l.count)+1])
			if end > 0 && now-end > l.config.idle {
				l.index.Delete(id)
				for i := range shard {
					atomic.StoreUint32(&shard[i], 0)
				}
				l.recycled = append(l.recycled, index)
			}
		}
		return true
	})

	println(now, len(l.recycled))

	atomic.StoreUint64(&l.config.recycled, uint64(len(l.recycled)))
	atomic.StoreUint32(&l.config.one, 0)
	atomic.StoreUint32(&l.config.run, 0)
}

func (l *Limit[T]) Clean() {
	if !atomic.CompareAndSwapUint32(&l.config.one, 0, 1) {
		return
	}

	l.index.Range(func(id T, index uint32) bool {
		l.index.Delete(id)
		l.recycled = append(l.recycled, index)
		return true
	})

	l.config.pool.Put(l.recycled...)
	l.recycled = l.recycled[:0]

	atomic.StoreUint32(&l.config.one, 0)
}

func (l *Limit[T]) Size() int {
	return l.index.Size()
}

func (l *Limit[T]) Recycled() int {
	return int(atomic.LoadUint64(&l.config.recycled))
}
