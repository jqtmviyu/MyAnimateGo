package peerfilter

import (
	"net/netip"
	"strings"
	"testing"
)

func TestParseAndMergeAddressFamilies(t *testing.T) {
	rows, err := ParseRanges(strings.NewReader("# source\n1.2.3.4\n1.2.3.0/24\n1.2.4.0-1.2.4.255\nanirss:2001:db8::1-2001:db8::3\nfd00::/126\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].First.String() != "1.2.3.0" || rows[0].Last.String() != "1.2.4.255" || !contains(rows, netip.MustParseAddr("fd00::3")) {
		t.Fatalf("ranges=%v", rows)
	}
	if contains(rows, netip.MustParseAddr("fd00::4")) {
		t.Fatal("CIDR extends outside prefix")
	}
}
func TestSubtractAllowRangesIncludingBoundaries(t *testing.T) {
	rows, err := ParseRanges(strings.NewReader("0.0.0.0/0\n::/0\n"))
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := ParseRanges(strings.NewReader("0.0.0.0\n1.2.3.0/24\n255.255.255.255\n::\nffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff\n"))
	if err != nil {
		t.Fatal(err)
	}
	result := subtract(rows, allowed)
	for _, ip := range []string{"0.0.0.0", "1.2.3.1", "255.255.255.255", "::", "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"} {
		if contains(result, netip.MustParseAddr(ip)) {
			t.Fatalf("allowed %s remains blocked", ip)
		}
	}
	for _, ip := range []string{"0.0.0.1", "1.2.4.0", "2001:db8::1"} {
		if !contains(result, netip.MustParseAddr(ip)) {
			t.Fatalf("unexpected removal %s", ip)
		}
	}
}
func TestPBHMatcherImportPreservesMeaning(t *testing.T) {
	rules, err := ImportRules("- '{\"method\":\"CONTAINS\",\"content\":\"Xunlei\"}'\n{\"method\":\"REGEX\",\"content\":\"^Thunder.*\"}")
	if err != nil || len(rules) != 2 {
		t.Fatalf("%v %v", rules, err)
	}
	if rules[0].Enabled {
		t.Fatal("import should await activation")
	}
	rules[0].Enabled = true
	m, err := compileRule(rules[0])
	if err != nil || !m.matches("Xunlei 1.0") || !m.matches("xunlei 1.0") {
		t.Fatal("matcher semantics changed")
	}
	rules[1].Enabled = true
	regex, err := compileRule(rules[1])
	if err != nil || !regex.matches("Thunder 1") || regex.matches("prefix Thunder 1") {
		t.Fatal("PBH regex whole-string semantics changed")
	}
	if _, err = ImportRules(`{"method":"CONTAINS","content":"safe","hit":"FALSE"}`); err == nil {
		t.Fatal("unsupported skip semantics silently became a ban")
	}
}
func BenchmarkNormalizeRanges(b *testing.B) {
	rows := make([]Range, 100000)
	for i := range rows {
		a := netip.AddrFrom4([4]byte{11, byte(i >> 16), byte(i >> 8), byte(i)})
		rows[i] = Range{a, a}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copyRows := append([]Range(nil), rows...)
		normalize(copyRows)
	}
}
