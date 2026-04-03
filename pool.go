package limit

import (
	"math"
	"runtime"
	"sync/atomic"
)

type slot struct {
	one      uint32
	_        [60]byte
	recycled []uint32
}

const (
	backSize = 8
	poolSlot = 1 << 6
	slotMask = poolSlot - 1
	ringSize = poolSlot / 1 * backSize
)

type Uint32Pool struct {
	pos   uint32
	_     [60]byte
	next  uint32
	_     [60]byte
	miss  uint64
	_     [56]byte
	slots [poolSlot]slot
}

func NewPool(initial ...uint32) *Uint32Pool {
	var slots [poolSlot]slot
	var size, next uint32
	for _, u := range initial {
		size += u
	}
	size /= poolSlot
	size = ((size + expandStep - 1) / expandStep) * expandStep
	for i := 0; i < poolSlot; i++ {
		slots[i].recycled = make([]uint32, 0, max(size, expandStep))
		for j := uint32(0); j < size; j++ {
			slots[i].recycled = append(slots[i].recycled, next)
			next++
		}
	}
	return &Uint32Pool{
		slots: slots,
		next:  next,
	}
}

func (p *Uint32Pool) Miss() uint64 { return atomic.LoadUint64(&p.miss) }

func (p *Uint32Pool) Get() (u uint32) {
	i := p.TryLock()
	z := len(p.slots[i].recycled)
	if z > 0 {
		z--
		u = p.slots[i].recycled[z]
		p.slots[i].recycled = p.slots[i].recycled[:z]
	} else {
		u = atomic.AddUint32(&p.next, 1)
		u--
	}
	p.Unlock(i)
	return u
}

func (p *Uint32Pool) Put(u ...uint32) {
	for len(u) > 0 {
		z := min(backSize, len(u))
		i := p.TryLock()
		p.slots[i].recycled = append(p.slots[i].recycled, u[:z]...)
		p.Unlock(i)
		u = u[z:]
	}
}

func (p *Uint32Pool) TryLock() (i uint32) {
	for {
		if i = atomic.AddUint32(&p.pos, 1) & slotMask; p.Lock(i) {
			return
		}
		atomic.AddUint64(&p.miss, 1)
		runtime.Gosched()
	}
}

func (p *Uint32Pool) Lock(i uint32) bool { return atomic.CompareAndSwapUint32(&p.slots[i].one, 0, 1) }

func (p *Uint32Pool) Unlock(i uint32) { atomic.StoreUint32(&p.slots[i].one, 0) }

func (p *Uint32Pool) Stats() (float64, float64, float64, uint64) {
	var t, v float64
	z := make([]float64, poolSlot)
	h := make([]uint32, poolSlot)
	for i := range h {
		h[i] = uint32(i)
	}
	for len(h) > 0 {
		j := 0
		k := len(h)
		for j < k {
			i := h[j]
			if p.Lock(i) {
				f := float64(len(p.slots[i].recycled))
				p.Unlock(i)
				z[i] = f
				t += f
				k--
				h[j] = h[k]
				h = h[:k]
				continue
			}
			j++
		}
		runtime.Gosched()
	}
	m := t / poolSlot
	for _, l := range z {
		d := l - m
		v += d * d
	}
	s := math.Sqrt(v / 64)
	c := s / m * 100
	return s, c, m, atomic.LoadUint64(&p.miss)
}
