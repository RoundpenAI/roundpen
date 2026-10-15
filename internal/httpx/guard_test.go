package httpx

import "testing"

func TestBlockedDialAddr(t *testing.T) {
	cases := []struct {
		addr  string
		block bool
	}{
		{"127.0.0.1:80", true},
		{"[::1]:80", true},
		{"[::ffff:127.0.0.1]:80", true},
		{"169.254.169.254:80", true},
		{"[fe80::1]:80", true},
		{"[::ffff:169.254.169.254]:80", true},
		{"0.0.0.0:80", true},
		{"[::]:80", true},
		{"224.0.0.1:80", true},
		{"[ff02::1]:80", true},
		{"example.com:443", true},
		{"[fe80::1%eth0]:80", true},
		{"10.1.2.3:80", true},
		{"172.16.9.9:80", true},
		{"192.168.1.10:443", true},
		{"[fd00::1]:443", true},
		{"8.8.8.8:443", false},
		{"[2606:4700:4700::1111]:443", false},
	}
	for _, tc := range cases {
		err := BlockedDialAddr(tc.addr, false, false)
		if tc.block && err == nil {
			t.Errorf("%s: expected blocked", tc.addr)
		}
		if !tc.block && err != nil {
			t.Errorf("%s: unexpected block: %v", tc.addr, err)
		}
	}
}

func TestBlockedDialAddrAllowPrivate(t *testing.T) {
	if err := BlockedDialAddr("10.1.2.3:80", false, true); err != nil {
		t.Fatalf("private address should be allowed when opted in: %v", err)
	}
	if err := BlockedDialAddr("10.1.2.3:80", false, false); err == nil {
		t.Fatal("private address must be blocked by default")
	}
}

func TestBlockedDialAddrAllowLoopback(t *testing.T) {
	if err := BlockedDialAddr("127.0.0.1:80", true, false); err != nil {
		t.Fatalf("loopback should be allowed when opted in: %v", err)
	}
	if err := BlockedDialAddr("169.254.169.254:80", true, false); err == nil {
		t.Fatal("metadata address must stay blocked even with allowLoopback")
	}
}
