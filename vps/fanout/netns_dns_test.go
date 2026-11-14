package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestNetnsDNS(t *testing.T, lookup func(context.Context, string, []string) ([]string, error)) *netnsDNS {
	t.Helper()
	if lookup == nil {
		lookup = func(context.Context, string, []string) ([]string, error) {
			return nil, errors.New("unexpected lookup")
		}
	}
	return &netnsDNS{
		ns:       "test",
		servers:  []string{"192.0.2.1:53"}, // TEST-NET-1，测试中不会真的拨号
		ttl:      time.Minute,
		negTTL:   time.Minute,
		cache:    map[string]netnsDNSEntry{},
		inflight: map[string]*netnsDNSCall{},
		lookup:   lookup,
	}
}

func TestNetnsDNSResolveCachesAndReturnsCopy(t *testing.T) {
	var calls int32
	d := newTestNetnsDNS(t, func(context.Context, string, []string) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		return []string{"198.51.100.7"}, nil
	})

	for i := 0; i < 3; i++ {
		ips, err := d.resolve(context.Background(), "example.com")
		if err != nil {
			t.Fatalf("resolve #%d: %v", i, err)
		}
		if len(ips) != 1 || ips[0] != "198.51.100.7" {
			t.Fatalf("resolve #%d returned %v", i, ips)
		}
		ips[0] = "mutated"
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lookup called %d times, want 1", got)
	}

	ips, err := d.resolve(context.Background(), "example.com")
	if err != nil {
		t.Fatal(err)
	}
	if ips[0] != "198.51.100.7" {
		t.Fatalf("cache was mutated by caller: %v", ips)
	}
}

func TestNetnsDNSCacheExpires(t *testing.T) {
	var calls int32
	d := newTestNetnsDNS(t, func(context.Context, string, []string) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		return []string{"198.51.100.7"}, nil
	})
	d.ttl = 10 * time.Millisecond

	if _, err := d.resolve(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := d.resolve(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("lookup called %d times, want 2", got)
	}
}

func TestNetnsDNSNegativeCache(t *testing.T) {
	var calls int32
	d := newTestNetnsDNS(t, func(context.Context, string, []string) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		return nil, errors.New("dns boom")
	})
	d.negTTL = 10 * time.Millisecond

	for i := 0; i < 2; i++ {
		if _, err := d.resolve(context.Background(), "missing.example"); err == nil {
			t.Fatalf("resolve #%d should fail", i)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lookup called %d times, want 1", got)
	}

	time.Sleep(40 * time.Millisecond)
	if _, err := d.resolve(context.Background(), "missing.example"); err == nil {
		t.Fatal("resolve after negative TTL should still fail")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("lookup called %d times after expiry, want 2", got)
	}
}

func TestNetnsDNSNoRecordIsNegative(t *testing.T) {
	var calls int32
	d := newTestNetnsDNS(t, func(context.Context, string, []string) ([]string, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	})

	for i := 0; i < 2; i++ {
		_, err := d.resolve(context.Background(), "empty.example")
		if !errors.Is(err, errDNSNoRecord) {
			t.Fatalf("resolve #%d err = %v, want errDNSNoRecord", i, err)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lookup called %d times, want 1", got)
	}
}

func TestNetnsDNSSingleflight(t *testing.T) {
	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})
	d := newTestNetnsDNS(t, func(context.Context, string, []string) ([]string, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			close(entered)
			<-release
		}
		return []string{"198.51.100.7"}, nil
	})

	const n = 16
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ips, err := d.resolve(context.Background(), "example.com")
			errs[i] = err
			if len(ips) > 0 {
				results[i] = ips[0]
			}
		}(i)
	}

	<-entered
	close(release)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("lookup called %d times, want 1", got)
	}
	for i := range results {
		if errs[i] != nil || results[i] != "198.51.100.7" {
			t.Fatalf("goroutine %d got (%q, %v)", i, results[i], errs[i])
		}
	}
}

func TestNetnsDNSClearDropsInFlightResult(t *testing.T) {
	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})
	d := newTestNetnsDNS(t, nil)
	d.lookup = func(context.Context, string, []string) ([]string, error) {
		if atomic.AddInt32(&calls, 1) == 2 {
			close(entered)
			<-release
		}
		return []string{"203.0.113.9"}, nil
	}

	if _, err := d.resolve(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	d.clear()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := d.resolve(context.Background(), "example.com"); err != nil {
			t.Errorf("in-flight resolve: %v", err)
		}
	}()
	<-entered
	d.clear() // 查询在途时隧道又重建了一次
	close(release)
	<-done

	if _, err := d.resolve(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("lookup called %d times, want 3 (stale generation must not be cached)", got)
	}
}

func TestNetnsDNSResolveRejectsEmptyHost(t *testing.T) {
	d := newTestNetnsDNS(t, nil)
	if _, err := d.resolve(context.Background(), ""); err == nil {
		t.Fatal("empty host should fail")
	}
}

func TestDNSServersFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		want []string
	}{
		{"", []string{"1.1.1.1:53", "8.8.8.8:53"}},
		{"9.9.9.9", []string{"9.9.9.9:53"}},
		{"1.0.0.1:5353, ,bad,::1", []string{"1.0.0.1:5353"}},
		{"  1.1.1.1 , 8.8.8.8:53 ", []string{"1.1.1.1:53", "8.8.8.8:53"}},
	}
	for _, c := range cases {
		t.Setenv("FANOUT_DNS_SERVERS", c.env)
		got := dnsServersFromEnv()
		if len(got) != len(c.want) {
			t.Fatalf("env %q: got %v, want %v", c.env, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("env %q: got %v, want %v", c.env, got, c.want)
			}
		}
	}
}

func TestDNSCacheTTLFromEnv(t *testing.T) {
	cases := []struct {
		env  string
		want time.Duration
	}{
		{"", defaultDNSCacheTTL},
		{"30", 30 * time.Second},
		{"0", 0},
		{"-1", defaultDNSCacheTTL},
		{"abc", defaultDNSCacheTTL},
		{"7200", defaultDNSCacheTTL},
	}
	for _, c := range cases {
		t.Setenv("FANOUT_DNS_TTL", c.env)
		if got := dnsCacheTTLFromEnv(); got != c.want {
			t.Fatalf("env %q: got %v, want %v", c.env, got, c.want)
		}
	}
}

func TestResolvConfContentUsesEnvServers(t *testing.T) {
	t.Setenv("FANOUT_DNS_SERVERS", "")
	want := "nameserver 1.1.1.1\nnameserver 8.8.8.8\noptions timeout:2 attempts:2\n"
	if got := resolvConfContent(); got != want {
		t.Fatalf("resolv.conf = %q, want %q", got, want)
	}

	t.Setenv("FANOUT_DNS_SERVERS", "9.9.9.9:5353")
	want = "nameserver 9.9.9.9\noptions timeout:2 attempts:2\n"
	if got := resolvConfContent(); got != want {
		t.Fatalf("resolv.conf = %q, want %q", got, want)
	}
}
