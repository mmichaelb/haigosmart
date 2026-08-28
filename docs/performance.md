# Reference measurements

**These are not a gate.** The constitution had a performance principle requiring
benchmark numbers in every PR; it was removed on 2026-08-28 (constitution v2.0.0)
because it did not match how this project is built. Nothing here has to be
defended in review, and CI does not run any of it.

What this file is for: when a change makes something *feel* slow, these are the
numbers it used to be, so you can tell whether you actually changed anything.
Re-run with `make bench`.

## Benchmarks

Apple M-series, darwin/arm64, Go 1.27, `-benchmem`:

```
BenchmarkPublish-8                    	 1182859	      1007 ns/op	     352 B/op	      11 allocs/op
BenchmarkFanOut30-8                   	  300690	      4000 ns/op	     352 B/op	      11 allocs/op
BenchmarkPublishStalledSubscriber-8   	 1641118	       729.6 ns/op	     352 B/op	      11 allocs/op
BenchmarkEventLine-8                  	 5461347	       219.1 ns/op	     176 B/op	       9 allocs/op
BenchmarkReadPacket-8           	24939265	        47.94 ns/op	     288 B/op	       1 allocs/op
BenchmarkDecodePropertyPost-8   	  739069	      1719 ns/op	     712 B/op	      11 allocs/op
BenchmarkDecodeConnect-8        	21653190	        57.92 ns/op	     332 B/op	       4 allocs/op
BenchmarkEncodeCommand-8        	  650589	      1850 ns/op	    1425 B/op	      38 allocs/op
BenchmarkEncodePublish-8        	31960723	        35.44 ns/op	     256 B/op	       2 allocs/op
BenchmarkStateDiff-8            	 7983747	       148.0 ns/op	     152 B/op	       6 allocs/op
```

## Soak

`go test ./internal/server -run TestSoak`: 30 concurrent fake bulbs, 20 rounds of
keep-alive plus state report each (600 state changes), with a display subscriber
deliberately never drained.

- Zero disconnections.
- Heap after the run: **~794 KiB**.
- 686 events shed from the stalled display queue, every one still in the log.

That last number is the interesting one, and it is a **correctness** result, not
a speed one: a terminal that stops reading must not be able to disconnect a bulb.
The soak test stays in the suite for that reason, and it runs under `-race` like
everything else.

## Notes, if you ever need them

`DecodePropertyPost` and `EncodeCommand` are the expensive calls at roughly
1.5-1.8 µs, both dominated by `encoding/json`. A bulb reports a handful of times
per minute, so thirty of them produce tens of these per second. There is no
reason to touch it.

`ReadPacket` at ~48 ns with one allocation runs on every frame from every bulb.
The one allocation is the frame buffer.

`BenchmarkPublishStalledSubscriber` being *faster* than the healthy path is the
right shape: the drop path does less work, and it is the path that runs on the
network goroutine when the display falls behind.
