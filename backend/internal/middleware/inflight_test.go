package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestInFlightLimiterCapsPerKey(t *testing.T) {
	l := NewInFlightLimiter(2)
	r1, ok1 := l.Acquire("a")
	_, ok2 := l.Acquire("a")
	_, ok3 := l.Acquire("a")
	_, okOther := l.Acquire("b")
	if !ok1 || !ok2 || ok3 || !okOther {
		t.Fatalf("got %v %v %v %v, want true true false true", ok1, ok2, ok3, okOther)
	}
	r1()
	r1() // idempotent
	if _, ok := l.Acquire("a"); !ok {
		t.Error("slot should be free after release")
	}
	if _, ok := l.Acquire("a"); ok {
		t.Error("double release must not free two slots")
	}
}

// Audit #2 VNDL-003: one IPv6 client controls a /64 and must not get a
// fresh rate-limit bucket per address.
func TestClientKeyGroupsIPv6By64(t *testing.T) {
	key := func(ip string) string {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("CF-Connecting-IP", ip)
		return ClientKey(req)
	}
	if a, b := key("2001:db8:1:2::1"), key("2001:db8:1:2:ffff::9"); a != b {
		t.Errorf("same /64 got different keys %q, %q", a, b)
	}
	if a, b := key("2001:db8:1:2::1"), key("2001:db8:1:3::1"); a == b {
		t.Errorf("different /64s share key %q", a)
	}
	if got := key("203.0.113.5"); got != "203.0.113.5" {
		t.Errorf("IPv4 key = %q", got)
	}
	if got := key("::ffff:203.0.113.5"); got != "::ffff:203.0.113.5" {
		t.Errorf("IPv4-mapped key = %q, want the address unchanged", got)
	}
}
