# limit
Go Rate Limiter

```go
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
```
```
cpu: 11th Gen Intel(R) Core(TM) i7-11700F @ 2.50GHz
BenchmarkLimit_Allow                     3721330               281.9 ns/op           121 B/op          0 allocs/op
BenchmarkLimit_Allow-4                   9900256               102.6 ns/op            50 B/op          0 allocs/op
BenchmarkLimit_Allow-8                  17491362                64.96 ns/op           28 B/op          0 allocs/op
BenchmarkLimit_Allow-16                 24842973                41.86 ns/op           20 B/op          0 allocs/op
BenchmarkLimit_Allow_Single             48721981                24.62 ns/op            0 B/op          0 allocs/op
BenchmarkLimit_Allow_Single-4           43432076                27.23 ns/op            0 B/op          0 allocs/op
BenchmarkLimit_Allow_Single-8           45159032                26.02 ns/op            0 B/op          0 allocs/op
BenchmarkLimit_Allow_Single-16          65817448                18.76 ns/op            0 B/op          0 allocs/op
PASS
```
