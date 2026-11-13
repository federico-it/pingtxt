package check

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultIPBlacklists are the DNS blacklists an rbl monitor checks IP
// addresses against when it lists none of its own.
var DefaultIPBlacklists = []string{
	"zen.spamhaus.org",
	"bl.spamcop.net",
	"b.barracudacentral.org",
	"bl.mailspike.net",
	"psbl.surriel.com",
	"ix.dnsbl.manitu.net",
	"dnsbl.dronebl.org",
	"all.s5h.net",
}

// DefaultDomainBlacklists are the domain blacklists (URI/RHSBL) a mail
// domain is checked against.
var DefaultDomainBlacklists = []string{
	"dbl.spamhaus.org",
	"multi.surbl.org",
	"multi.uribl.com",
}

// domainLists are well-known lists that take domain names, not addresses.
var domainLists = map[string]bool{
	"dbl.spamhaus.org": true, "multi.surbl.org": true, "multi.uribl.com": true,
	"black.uribl.com": true, "dbl.nordspam.com": true, "rhsbl.sorbs.net": true,
}

// maxRBLQueries caps the DNS lookups of one check (addresses x lists).
const maxRBLQueries = 200

// ParseBlacklists splits a list of blacklist zones (commas, spaces or new
// lines). Empty means the defaults.
func ParseBlacklists(s string) ([]string, error) {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == ';' }) {
		z := strings.ToLower(strings.Trim(f, "."))
		if !validDomain(z) {
			return nil, fmt.Errorf("%q is not a blacklist zone", f)
		}
		if !slices.Contains(out, z) {
			out = append(out, z)
		}
	}
	if len(out) > 50 {
		return nil, errors.New("at most 50 blacklists")
	}
	return out, nil
}

func validDomain(s string) bool {
	if len(s) < 3 || len(s) > 253 || !strings.Contains(s, ".") {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
				return false
			}
		}
	}
	return true
}

// RBLTarget checks the target of an rbl monitor: an IPv4/IPv6 address or a
// mail domain.
func RBLTarget(target string) (ip net.IP, domain string, err error) {
	t := strings.ToLower(strings.TrimSuffix(StripBrackets(strings.TrimSpace(target)), "."))
	if ip := net.ParseIP(t); ip != nil {
		return ip, "", nil
	}
	if !validDomain(t) {
		return nil, "", errors.New("target must be an IP address or a mail domain")
	}
	return nil, t, nil
}

type rblListing struct {
	what, list, code string
}

// runRBL looks the target up in DNS blacklists. An IP address is checked
// against the IP lists. A domain is checked against the domain lists, and
// the addresses of its mail servers (MX, or the domain itself without MX)
// against the IP lists. The check fails when any list has an entry.
// Keyword holds the lists to use (empty: DefaultIPBlacklists and
// DefaultDomainBlacklists); DNSServer an optional resolver.
func runRBL(ctx context.Context, j Job) Result {
	ip, domain, err := RBLTarget(j.Target)
	if err != nil {
		return Result{Message: err.Error()}
	}
	custom, err := ParseBlacklists(j.Keyword)
	if err != nil {
		return Result{Message: err.Error()}
	}
	ipLists, domLists := DefaultIPBlacklists, DefaultDomainBlacklists
	if len(custom) > 0 {
		ipLists, domLists = nil, nil
		for _, z := range custom {
			if domainLists[z] {
				domLists = append(domLists, z)
			} else {
				ipLists = append(ipLists, z)
			}
		}
	}
	res := j.resolver()
	start := time.Now()
	type query struct{ name, what, list string }
	var qs []query
	var ips []net.IP
	if ip != nil {
		ips = []net.IP{ip}
	} else {
		for _, l := range domLists {
			qs = append(qs, query{domain + "." + l, domain, l})
		}
		ips, err = mailServerIPs(ctx, res, domain, j.IPFamily)
		if err != nil {
			return Result{LatencyMs: ms(time.Since(start)), Message: err.Error()}
		}
	}
	for _, a := range ips {
		rev := reverseIP(a)
		for _, l := range ipLists {
			qs = append(qs, query{rev + "." + l, a.String(), l})
		}
	}
	if len(qs) == 0 {
		return Result{LatencyMs: ms(time.Since(start)), Message: "no blacklist to check"}
	}
	if len(qs) > maxRBLQueries {
		qs = qs[:maxRBLQueries]
	}
	var (
		mu     sync.Mutex
		hits   []rblListing
		failed int
		wg     sync.WaitGroup
		sem    = make(chan struct{}, 16)
	)
	for _, q := range qs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			addrs, err := res.LookupHost(ctx, q.name)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				var de *net.DNSError
				if !errors.As(err, &de) || !de.IsNotFound {
					failed++
				}
				return
			}
			if code, listed := rblListed(addrs); listed {
				hits = append(hits, rblListing{q.what, q.list, code})
			} else {
				failed++ // refused or rate limited: no answer for this list
			}
		}()
	}
	wg.Wait()
	lat := ms(time.Since(start))
	if ctx.Err() != nil && len(hits) == 0 {
		return Result{LatencyMs: lat, Message: "timeout"}
	}
	if failed == len(qs) {
		return Result{LatencyMs: lat, Message: "no blacklist answered (the resolver may be blocked by the lists: set one)"}
	}
	if len(hits) > 0 {
		slices.SortFunc(hits, func(a, b rblListing) int {
			return strings.Compare(a.what+a.list, b.what+b.list)
		})
		parts := make([]string, len(hits))
		for i, h := range hits {
			parts[i] = h.list
			if h.what != j.Target {
				parts[i] = h.what + " on " + h.list
			}
			parts[i] += " (" + h.code + ")"
		}
		return Result{LatencyMs: lat, Message: "listed: " + strings.Join(parts, ", ")}
	}
	msg := fmt.Sprintf("not listed on %d lists", len(ipLists)+len(domLists))
	if domain != "" {
		msg += fmt.Sprintf(" (%d mail server addresses)", len(ips))
	}
	if failed > 0 {
		msg += fmt.Sprintf(", %d queries unanswered", failed)
	}
	return Result{OK: true, LatencyMs: lat, Message: msg}
}

// rblListed reads a blacklist answer. Entries are 127.0.0.2 and up; lists
// answer 127.0.0.1 or 127.255.255.x (Spamhaus) when they refuse the query,
// e.g. from a public resolver: that is not a listing.
func rblListed(addrs []string) (string, bool) {
	for _, a := range addrs {
		ip := net.ParseIP(a).To4()
		if ip == nil || ip[0] != 127 {
			continue
		}
		if ip[1] == 255 && ip[2] == 255 || ip.Equal(net.IPv4(127, 0, 0, 1)) || ip[3] == 255 {
			continue
		}
		return a, true
	}
	return "", false
}

// mailServerIPs returns the addresses of the domain's MX hosts, or of the
// domain itself when it has no MX record.
func mailServerIPs(ctx context.Context, res *net.Resolver, domain string, family int) ([]net.IP, error) {
	hosts := []string{domain}
	mx, err := res.LookupMX(ctx, domain)
	if err == nil && len(mx) > 0 {
		hosts = hosts[:0]
		for _, m := range mx {
			h := strings.TrimSuffix(m.Host, ".")
			if h != "" && !slices.Contains(hosts, h) {
				hosts = append(hosts, h)
			}
		}
	} else if err != nil {
		var de *net.DNSError
		if !errors.As(err, &de) || !de.IsNotFound {
			return nil, fmt.Errorf("mx lookup: %s", cleanErr(err))
		}
	}
	if len(hosts) > 10 {
		hosts = hosts[:10]
	}
	var out []net.IP
	for _, h := range hosts {
		addrs, err := res.LookupIPAddr(ctx, h)
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if family == IPv4 && a.IP.To4() == nil || family == IPv6 && a.IP.To4() != nil {
				continue
			}
			if !slices.ContainsFunc(out, a.IP.Equal) {
				out = append(out, a.IP)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no address for the mail servers of %s", domain)
	}
	return out, nil
}

// reverseIP writes an address the way blacklists are queried: 1.2.3.4 is
// 4.3.2.1, IPv6 addresses are reversed nibbles.
func reverseIP(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0])
	}
	const hexd = "0123456789abcdef"
	v6 := ip.To16()
	b := make([]byte, 0, 63)
	for i := len(v6) - 1; i >= 0; i-- {
		if len(b) > 0 {
			b = append(b, '.')
		}
		b = append(b, hexd[v6[i]&0xf], '.', hexd[v6[i]>>4])
	}
	return string(b)
}
