package allocator

import (
	"net"
	"testing"

	"go.universe.tf/metallb/internal/config"
)

func TestAssignment(t *testing.T) {
	alloc := New()
	if err := alloc.SetPools(map[string]*config.Pool{
		"test": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.4/31")},
		},
		"test2": {
			AvoidBuggyIPs: true,
			AutoAssign:    true,
			CIDR:          []*net.IPNet{ipnet("1.2.4.0/24")},
		},
	}); err != nil {
		t.Fatalf("SetPools: %s", err)
	}

	tests := []struct {
		desc    string
		svc     string
		ip      string
		wantErr bool
	}{
		{
			desc: "assign s1",
			svc:  "s1",
			ip:   "1.2.3.4",
		},
		{
			desc: "s1 idempotent reassign",
			svc:  "s1",
			ip:   "1.2.3.4",
		},
		{
			desc:    "s2 can't grab s1's IP",
			svc:     "s2",
			ip:      "1.2.3.4",
			wantErr: true,
		},
		{
			desc: "s2 can get the other IP",
			svc:  "s2",
			ip:   "1.2.3.5",
		},
		{
			desc:    "s1 now can't grab s2's IP",
			svc:     "s1",
			ip:      "1.2.3.5",
			wantErr: true,
		},
		{
			desc: "s1 frees its IP",
			svc:  "s1",
			ip:   "",
		},
		{
			desc: "s2 can grab s1's former IP",
			svc:  "s2",
			ip:   "1.2.3.4",
		},
		{
			desc: "s1 can now grab s2's former IP",
			svc:  "s1",
			ip:   "1.2.3.5",
		},
		{
			desc:    "s3 cannot grab a 0 buggy IP",
			svc:     "s3",
			ip:      "1.2.4.0",
			wantErr: true,
		},
		{
			desc:    "s3 cannot grab a 255 buggy IP",
			svc:     "s3",
			ip:      "1.2.4.255",
			wantErr: true,
		},
		{
			desc: "s3 can grab another IP in that pool",
			svc:  "s3",
			ip:   "1.2.4.254",
		},
	}

	for _, test := range tests {
		if test.ip == "" {
			alloc.Unassign(test.svc)
			continue
		}
		ip := net.ParseIP(test.ip)
		if ip == nil {
			t.Fatalf("invalid IP %q in test %q", test.ip, test.desc)
		}
		err := alloc.Assign(test.svc, ip)
		if test.wantErr {
			if err == nil {
				t.Errorf("%q should have caused an error, but did not", test.desc)
			} else if a := assigned(alloc, test.svc); a == test.ip {
				t.Errorf("%q: Assign(%q, %q) failed, but allocator did record allocation", test.desc, test.svc, test.ip)
			}

			continue
		}

		if err != nil {
			t.Errorf("%q: Assign(%q, %q): %s", test.desc, test.svc, test.ip, err)
		}
		if a := assigned(alloc, test.svc); a != test.ip {
			t.Errorf("%q: ran Assign(%q, %q), but allocator has recorded allocation of %q", test.desc, test.svc, test.ip, a)
		}
	}
}

func TestPoolAllocation(t *testing.T) {
	alloc := New()
	// This test only allocates from the "test" pool, so it will run
	// out of IPs quickly even though there are tons available in
	// other pools.
	if err := alloc.SetPools(map[string]*config.Pool{
		"not_this_one": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("192.168.0.0/16")},
		},
		"test": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.4/31"), ipnet("1.2.3.10/31")},
		},
		"test2": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("10.20.30.0/24")},
		},
	}); err != nil {
		t.Fatalf("SetPools: %s", err)
	}

	validIPs := map[string]bool{
		"1.2.3.4":  true,
		"1.2.3.5":  true,
		"1.2.3.10": true,
		"1.2.3.11": true,
	}

	tests := []struct {
		svc      string
		unassign bool
		wantErr  bool
	}{
		{svc: "s1"},
		{svc: "s2"},
		{svc: "s3"},
		{svc: "s4"},
		{
			svc:     "s5",
			wantErr: true,
		},
		{
			svc:     "s6",
			wantErr: true,
		},
		{
			svc:      "s1",
			unassign: true,
		},
		{svc: "s5"},
		{
			svc:     "s6",
			wantErr: true,
		},
	}

	for i, test := range tests {
		if test.unassign {
			alloc.Unassign(test.svc)
			continue
		}
		ip, err := alloc.AllocateFromPool(test.svc, "test")
		if test.wantErr {
			if err == nil {
				t.Errorf("#%d should have caused an error, but did not", i+1)

			}
			continue
		}
		if err != nil {
			t.Errorf("#%d AllocateFromPool(%q, \"test\"): %s", i+1, test.svc, err)
		}
		if !validIPs[ip.String()] {
			t.Errorf("#%d allocated unexpected IP %q", i+1, ip)
		}
	}

	alloc.Unassign("s5")
	if _, err := alloc.AllocateFromPool("s5", "nonexistentpool"); err == nil {
		t.Error("Allocating from non-existent pool succeeded")
	}
}

func TestAllocation(t *testing.T) {
	alloc := New()
	if err := alloc.SetPools(map[string]*config.Pool{
		"test1": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.4/31")},
		},
		"test2": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.10/31")},
		},
	}); err != nil {
		t.Fatalf("SetPools: %s", err)
	}

	validIPs := map[string]bool{
		"1.2.3.4":  true,
		"1.2.3.5":  true,
		"1.2.3.10": true,
		"1.2.3.11": true,
	}

	tests := []struct {
		svc      string
		unassign bool
		wantErr  bool
	}{
		{svc: "s1"},
		{svc: "s2"},
		{svc: "s3"},
		{svc: "s4"},
		{
			svc:     "s5",
			wantErr: true,
		},
		{
			svc:     "s6",
			wantErr: true,
		},
		{
			svc:      "s1",
			unassign: true,
		},
		{svc: "s5"},
		{
			svc:     "s6",
			wantErr: true,
		},
	}

	for i, test := range tests {
		if test.unassign {
			alloc.Unassign(test.svc)
			continue
		}
		ip, err := alloc.Allocate(test.svc)
		if test.wantErr {
			if err == nil {
				t.Errorf("#%d should have caused an error, but did not", i+1)

			}
			continue
		}
		if err != nil {
			t.Errorf("#%d Allocate(%q, \"test\"): %s", i+1, test.svc, err)
		}
		if !validIPs[ip.String()] {
			t.Errorf("#%d allocated unexpected IP %q", i+1, ip)
		}
	}
}

func TestBuggyIPs(t *testing.T) {
	alloc := New()
	if err := alloc.SetPools(map[string]*config.Pool{
		"test": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.0/31")},
		},
		"test2": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.254/31")},
		},
		"test3": {
			AvoidBuggyIPs: true,
			AutoAssign:    true,
			CIDR:          []*net.IPNet{ipnet("1.2.4.0/31")},
		},
		"test4": {
			AvoidBuggyIPs: true,
			AutoAssign:    true,
			CIDR:          []*net.IPNet{ipnet("1.2.4.254/31")},
		},
	}); err != nil {
		t.Fatalf("SetPools: %s", err)
	}

	validIPs := map[string]bool{
		"1.2.3.0":   true,
		"1.2.3.1":   true,
		"1.2.3.254": true,
		"1.2.3.255": true,
		"1.2.4.1":   true,
		"1.2.4.254": true,
	}

	tests := []struct {
		svc     string
		wantErr bool
	}{
		{svc: "s1"},
		{svc: "s2"},
		{svc: "s3"},
		{svc: "s4"},
		{svc: "s5"},
		{svc: "s6"},
		{
			svc:     "s7",
			wantErr: true,
		},
	}

	for i, test := range tests {
		ip, err := alloc.Allocate(test.svc)
		if test.wantErr {
			if err == nil {
				t.Errorf("#%d should have caused an error, but did not", i+1)

			}
			continue
		}
		if err != nil {
			t.Errorf("#%d Allocate(%q, \"test\"): %s", i+1, test.svc, err)
		}
		if !validIPs[ip.String()] {
			t.Errorf("#%d allocated unexpected IP %q", i+1, ip)
		}
	}

}

func TestConfigReload(t *testing.T) {
	alloc := New()
	if err := alloc.SetPools(map[string]*config.Pool{
		"test": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.0/30")},
		},
	}); err != nil {
		t.Fatalf("SetPools: %s", err)
	}
	if err := alloc.Assign("s1", net.ParseIP("1.2.3.0")); err != nil {
		t.Fatalf("Assign(s1, 1.2.3.0): %s", err)
	}

	tests := []struct {
		desc    string
		pools   map[string]*config.Pool
		wantErr bool
		pool    string // Pool that 1.2.3.0 should be in
	}{
		{
			desc: "set same config is no-op",
			pools: map[string]*config.Pool{
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/30")},
				},
			},
			pool: "test",
		},
		{
			desc: "expand pool",
			pools: map[string]*config.Pool{
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/24")},
				},
			},
			pool: "test",
		},
		{
			desc: "shrink pool",
			pools: map[string]*config.Pool{
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/30")},
				},
			},
			pool: "test",
		},
		{
			desc: "can't shrink further",
			pools: map[string]*config.Pool{
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.2/31")},
				},
			},
			pool:    "test",
			wantErr: true,
		},
		{
			desc: "rename the pool",
			pools: map[string]*config.Pool{
				"test2": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/30")},
				},
			},
			pool: "test2",
		},
		{
			desc: "split pool",
			pools: map[string]*config.Pool{
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/31")},
				},
				"test2": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.2/31")},
				},
			},
			pool: "test",
		},
		{
			desc: "swap pool names",
			pools: map[string]*config.Pool{
				"test2": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/31")},
				},
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.2/31")},
				},
			},
			pool: "test2",
		},
		{
			desc: "delete used pool",
			pools: map[string]*config.Pool{
				"test": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.2/31")},
				},
			},
			pool:    "test2",
			wantErr: true,
		},
		{
			desc: "delete unused pool",
			pools: map[string]*config.Pool{
				"test2": {
					AutoAssign: true,
					CIDR:       []*net.IPNet{ipnet("1.2.3.0/31")},
				},
			},
			pool: "test2",
		},
		{
			desc: "enable buggy IPs not allowed",
			pools: map[string]*config.Pool{
				"test2": {
					AutoAssign:    true,
					AvoidBuggyIPs: true,
					CIDR:          []*net.IPNet{ipnet("1.2.3.0/31")},
				},
			},
			pool:    "test2",
			wantErr: true,
		},
	}

	for _, test := range tests {
		err := alloc.SetPools(test.pools)
		if test.wantErr {
			if err == nil {
				t.Errorf("%q should have failed to SetPools, but succeeded", test.desc)
			}
		} else if err != nil {
			t.Errorf("%q failed to SetPools: %s", test.desc, err)
		}
		gotPool := assignedPool(alloc, "s1")
		if gotPool != test.pool {
			t.Errorf("%q: s1 is in wrong pool, want %q, got %q", test.desc, test.pool, gotPool)
		}
	}
}

func TestAutoAssign(t *testing.T) {
	alloc := New()
	if err := alloc.SetPools(map[string]*config.Pool{
		"test1": {
			AutoAssign: false,
			CIDR:       []*net.IPNet{ipnet("1.2.3.4/31")},
		},
		"test2": {
			AutoAssign: true,
			CIDR:       []*net.IPNet{ipnet("1.2.3.10/31")},
		},
	}); err != nil {
		t.Fatalf("SetPools: %s", err)
	}

	validIPs := map[string]bool{
		"1.2.3.4":  false,
		"1.2.3.5":  false,
		"1.2.3.10": true,
		"1.2.3.11": true,
	}

	tests := []struct {
		svc     string
		wantErr bool
	}{
		{svc: "s1"},
		{svc: "s2"},
		{
			svc:     "s3",
			wantErr: true,
		},
		{
			svc:     "s4",
			wantErr: true,
		},
		{
			svc:     "s5",
			wantErr: true,
		},
	}

	for i, test := range tests {
		ip, err := alloc.Allocate(test.svc)
		if test.wantErr {
			if err == nil {
				t.Errorf("#%d should have caused an error, but did not", i+1)
			}
			continue
		}
		if err != nil {
			t.Errorf("#%d Allocate(%q, \"test\"): %s", i+1, test.svc, err)
		}
		if !validIPs[ip.String()] {
			t.Errorf("#%d allocated unexpected IP %q", i+1, ip)
		}
	}
}

func TestPoolCount(t *testing.T) {
	tests := []struct {
		desc string
		pool *config.Pool
		want int64
	}{
		{
			desc: "BGP /24",
			pool: &config.Pool{
				Protocol: config.BGP,
				CIDR:     []*net.IPNet{ipnet("1.2.3.0/24")},
			},
			want: 256,
		},
		{
			desc: "BGP /24 and /25",
			pool: &config.Pool{
				Protocol: config.BGP,
				CIDR:     []*net.IPNet{ipnet("1.2.3.0/24"), ipnet("2.3.4.128/25")},
			},
			want: 384,
		},
		{
			desc: "BGP /24 and /25, no buggy IPs",
			pool: &config.Pool{
				Protocol:      config.BGP,
				CIDR:          []*net.IPNet{ipnet("1.2.3.0/24"), ipnet("2.3.4.128/25")},
				AvoidBuggyIPs: true,
			},
			want: 381,
		},
	}

	for _, test := range tests {
		got := poolCount(test.pool)
		if test.want != got {
			t.Errorf("%q: wrong pool count, want %d, got %d", test.desc, test.want, got)
		}
	}
}

// Some helpers

// Peeks inside Allocator to find the allocated IP and pool for a service.
func assigned(a *Allocator, svc string) string {
	if a.svcToIP[svc] == nil {
		return ""
	}
	return a.svcToIP[svc].String()
}

func assignedPool(a *Allocator, svc string) string {
	return a.svcToPool[svc]
}

func ipnet(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}
