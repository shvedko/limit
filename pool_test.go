package limit_test

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/shvedko/limit"
)

func TestUint32Pool_Put(t *testing.T) {
	var wg sync.WaitGroup
	p := limit.NewPool()
	w := runtime.GOMAXPROCS(0)
	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := make([]uint32, 0, 100_000)
			for j := 0; j < 1_000_000; j++ {
				n := p.Get()
				u = append(u, n)
				if len(u) == cap(u) {
					p.Put(u...)
					u = u[:0]
				}
			}
		}()
	}
	wg.Wait()
	fmt.Println(p.Miss())
	fmt.Println(p.Stats())
}
