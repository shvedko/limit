package limit

import (
	"math"
	"runtime"
	"sync/atomic"
)

type slot struct {
	b uint32
	_ [60]byte // Padding: добиваем до 64 байт (размер кэш-линии)
	k []uint32
}

type Pool2 struct {
	//n uint32
	//i uint32
	//	b [64]uint32
	//	k [64][]uint32
	//m uint64
	i uint32
	_ [60]byte
	n uint32
	_ [60]byte
	m uint64
	_ [56]byte // 8 байт, значит 64-8=56
	s [64]slot
}

func (p *Pool2) Miss() uint64 { return atomic.LoadUint64(&p.m) }

func (p *Pool2) Get() (k uint32) {
	i := p.Lock()
	z := len(p.s[i].k)
	if z > 0 {
		z--
		k = p.s[i].k[z]
		p.s[i].k = p.s[i].k[:z]
	} else {
		k = atomic.AddUint32(&p.n, 1)
		k--
	}
	p.Unlock(i)
	return k
}

func (p *Pool2) Put(k ...uint32) {
	for len(k) > 0 {
		z := min(8, len(k))
		i := p.Lock()
		p.s[i].k = append(p.s[i].k, k[:z]...)
		p.Unlock(i)
		k = k[z:]
	}
}

func (p *Pool2) Lock() (i uint32) {
	for {
		if i = atomic.AddUint32(&p.i, 1) & 63; atomic.CompareAndSwapUint32(&p.s[i].b, 0, 1) {
			break
		}
		atomic.AddUint64(&p.m, 1)
		runtime.Gosched()
	}
	return
}

func (p *Pool2) Unlock(i uint32) { atomic.StoreUint32(&p.s[i].b, 0) }

func NewPool() *Pool2 {
	var s [64]slot
	for i := 0; i < 64; i++ {
		s[i].k = make([]uint32, 0, expandStep/64)
	}
	return &Pool2{
		s: s,
	}
}

func (p *Pool2) Stats() (float64, float64, float64) {
	var t float64
	ll := make([]float64, 64)

	for i := 0; i < 64; i++ {
		l := float64(len(p.s[i].k))
		ll[i] = l
		t += l
	}

	m := t / 64

	var v float64
	for _, l := range ll {
		d := l - m
		v += d * d
	}

	sd := math.Sqrt(v / 64)
	cv := sd / m * 100

	return sd, cv, m
}
