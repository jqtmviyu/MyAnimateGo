package peerfilter

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"sort"
	"strings"
)

type Range struct {
	First netip.Addr `json:"first"`
	Last  netip.Addr `json:"last"`
}

func address(s string) (netip.Addr, error) {
	a, e := netip.ParseAddr(strings.TrimSpace(s))
	if e == nil {
		a = a.Unmap()
		if a.Zone() != "" {
			return netip.Addr{}, errors.New("地址不能带接口范围")
		}
	}
	return a, e
}
func bareRange(s string) (Range, error) {
	if strings.Contains(s, "/") {
		p, e := netip.ParsePrefix(s)
		if e != nil {
			return Range{}, e
		}
		if p.Addr().Is4In6() {
			return Range{}, errors.New("请使用普通IPv4或IPv6网段")
		}
		p = p.Masked()
		first := p.Addr()
		b := first.As16()
		start := 0
		if first.Is4() {
			start = 12
		}
		for bit := p.Bits(); bit < first.BitLen(); bit++ {
			i := start + bit/8
			b[i] |= byte(1 << (7 - bit%8))
		}
		last := netip.AddrFrom16(b)
		if first.Is4() {
			last = last.Unmap()
		}
		return Range{first, last}, nil
	}
	left, right, found := strings.Cut(s, "-")
	a, e := address(left)
	if e != nil {
		return Range{}, e
	}
	b := a
	if found {
		b, e = address(right)
		if e != nil {
			return Range{}, e
		}
	}
	if a.BitLen() != b.BitLen() || a.Compare(b) > 0 {
		return Range{}, errors.New("起止地址类型或顺序错误")
	}
	return Range{a, b}, nil
}
func parseRange(s string) (Range, error) {
	s = strings.TrimSpace(s)
	if part, _, found := strings.Cut(s, ","); found {
		s = strings.TrimSpace(part)
	}
	if r, e := bareRange(s); e == nil {
		return r, nil
	}
	// P2P plaintext 的名称和IPv6地址均含冒号，逐个分隔点尝试合法地址范围。
	for i, c := range s {
		if c == ':' {
			if r, e := bareRange(strings.TrimSpace(s[i+1:])); e == nil {
				return r, nil
			}
		}
	}
	return Range{}, fmt.Errorf("无法解析IP、CIDR或地址范围：%s", s)
}
func ParseRanges(reader io.Reader) ([]Range, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 256<<10)
	rows := []Range{}
	n := 0
	for scanner.Scan() {
		n++
		s := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if i := strings.Index(s, "#"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		if s == "" || strings.HasPrefix(s, "//") {
			continue
		}
		r, e := parseRange(s)
		if e != nil {
			return nil, fmt.Errorf("第%d行：%w", n, e)
		}
		rows = append(rows, r)
	}
	if e := scanner.Err(); e != nil {
		return nil, e
	}
	return normalize(rows), nil
}
func normalize(rows []Range) []Range {
	if len(rows) == 0 {
		return []Range{}
	}
	sort.Slice(rows, func(i, j int) bool {
		c := rows[i].First.Compare(rows[j].First)
		if c == 0 {
			return rows[i].Last.Compare(rows[j].Last) < 0
		}
		return c < 0
	})
	out := rows[:0]
	for _, r := range rows {
		if len(out) == 0 {
			out = append(out, r)
			continue
		}
		p := &out[len(out)-1]
		next := p.Last.Next()
		if p.First.BitLen() == r.First.BitLen() && (r.First.Compare(p.Last) <= 0 || next.IsValid() && r.First == next) {
			if r.Last.Compare(p.Last) > 0 {
				p.Last = r.Last
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}
func contains(rows []Range, a netip.Addr) bool {
	i := sort.Search(len(rows), func(i int) bool { return rows[i].Last.Compare(a) >= 0 })
	return i < len(rows) && rows[i].First.BitLen() == a.BitLen() && rows[i].First.Compare(a) <= 0
}
func subtract(rows, allowed []Range) []Range {
	out := []Range{}
	j := 0
	for _, r := range rows {
		for j < len(allowed) && allowed[j].Last.Compare(r.First) < 0 {
			j++
		}
		first := r.First
		done := false
		for k := j; k < len(allowed) && allowed[k].First.Compare(r.Last) <= 0; k++ {
			a := allowed[k]
			if a.First.BitLen() != r.First.BitLen() {
				continue
			}
			if a.First.Compare(first) > 0 {
				out = append(out, Range{first, a.First.Prev()})
			}
			if a.Last.Compare(r.Last) >= 0 {
				done = true
				break
			}
			if a.Last.Compare(first) >= 0 {
				first = a.Last.Next()
			}
		}
		if !done {
			out = append(out, Range{first, r.Last})
		}
	}
	return out
}
