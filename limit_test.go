package limit_test

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"

	"github.com/shvedko/limit"
)

func TestLimit_Index(t *testing.T) {
	type args struct {
		id int
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
		{
			name: "",
			args: args{id: 0},
			want: 0,
		},
		{
			name: "",
			args: args{id: 1},
			want: 1,
		},
		{
			name: "",
			args: args{id: 2},
			want: 2,
		},
		{
			name: "",
			args: args{id: 0},
			want: 0,
		},
		{
			name: "",
			args: args{id: 1},
			want: 1,
		},
		{
			name: "",
			args: args{id: 2},
			want: 2,
		},
	}
	l := limit.New[int](100, 60)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := l.Index(tt.args.id); got != tt.want {
				t.Errorf("Index() = %v, want %v", got, tt.want)
			}
		})
	}
}

func BenchmarkLimit_Index(b *testing.B) {
	p := limit.NewPool()
	l := limit.New[int](100, 60, limit.WithPool(p))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Index(rand.Intn(1_000_000))
		}
	})
	b.ReportMetric(float64(p.Miss()), "miss")
}

func BenchmarkLimit_Allow(b *testing.B) {
	p := limit.NewPool()
	l := limit.New[int](100, 60, limit.WithPool(p))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow(rand.Intn(1_000_000))
		}
	})
	b.ReportMetric(float64(p.Miss()), "miss")
}

func BenchmarkLimit_Allow_Single(b *testing.B) {
	p := limit.NewPool()
	l := limit.New[int](100, 60, limit.WithPool(p))
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			l.Allow(42)
		}
	})
	b.ReportMetric(float64(p.Miss()), "miss")
}

func ExampleLimit_Allow() {
	l := limit.New[int](3, 60, limit.WithJanitor(300, 3200))
	for i := 0; i < 5; i++ {
		fmt.Println(l.Allow(123))
	}
	// Output:
	// true
	// true
	// true
	// false
	// false
}

func TestLimit_Allow(t *testing.T) {
	t.Skip()
	var wg sync.WaitGroup
	p := limit.NewPool()
	l := limit.New[int](3, 1, limit.WithJanitor(2, 3), limit.WithJanitorThreshold(.1), limit.WithPool(p))
	w := runtime.GOMAXPROCS(0)
	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5_000_000; j++ {
				for k := 0; k < 50; k++ {
					l.Allow(j + k)
				}
			}
		}()
	}
	wg.Wait()
	t.Log(
		l.Size(), l.Recycled())
	t.Log(
		p.Stats())
}
