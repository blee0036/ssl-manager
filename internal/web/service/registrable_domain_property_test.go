package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// Feature: domain-expiry-monitor, Property 1: 可注册域名识别为 eTLD+1
//
// 随机标签 + 公共后缀（含单级 com/net/org 与多级 co.uk/com.cn/com.au）拼成的
// 主机名，RegistrableDomain 返回其 eTLD+1；子域名输入归一到相同的 eTLD+1（即
// WHOIS 查询目标 = eTLD+1，不将子域名作为独立监控对象）；公共后缀本身或语法
// 非法的输入返回包裹 ErrInvalidDomain 的错误。
//
// 属性 1e 进一步覆盖服务端 DNS 名称语法校验：非法字符集（如 _）、首/尾连字符、
// 超长 label、超长全名、IP 字面量、带 scheme/端口的字符串等都必须被拒绝
// （在 publicsuffix 计算之前挡下，避免非法监控项被登记）。
//
// 被测函数 RegistrableDomain / 哨兵错误 ErrInvalidDomain 定义在同包
// internal/web/service/registrable_domain.go，故本测试直接引用。
//
// **Validates: Requirements 3.3, 4.1, 4.2, 4.8**
func TestProperty_RegistrableDomainIsETLDPlusOne(t *testing.T) {
	parameters := gopter.DefaultTestParameters()
	parameters.MinSuccessfulTests = 100
	// Fixed seed => deterministic input set (matches the package convention),
	// so a passing run is reproducible rather than flaky.
	parameters.Rng.Seed(42)

	properties := gopter.NewProperties(parameters)

	// Public suffixes covering single-level (com/net/org) and the multi-level
	// suffixes called out by requirement 4.2 (co.uk/com.cn/com.au).
	suffixGen := gen.OneConstOf("com", "net", "org", "co.uk", "com.cn", "com.au")

	// A registrable label: a valid DNS label of lowercase letters/digits that
	// starts with a letter (length 5-15). It is long enough that colliding with a
	// Public Suffix List entry (e.g. a private-section brand such as
	// "blogspot.com") is negligible; combined with the fixed RNG seed the whole
	// input set is deterministic.
	regLabelGen := gen.RegexMatch("[a-z][a-z0-9]{4,14}")

	// A subdomain label prepended in front of the registrable domain (length 1-10).
	subLabelGen := gen.RegexMatch("[a-z][a-z0-9]{0,9}")

	// Property 1a: label.<suffix> is itself the eTLD+1 (needs correct handling of
	// multi-level suffixes so that e.g. example.com.cn -> example.com.cn, not com.cn).
	properties.Property("a registrable label under a public suffix resolves to that label.suffix as eTLD+1", prop.ForAll(
		func(label, suffix string) bool {
			host := label + "." + suffix
			got, err := RegistrableDomain(host)
			if err != nil {
				t.Logf("host %q: unexpected error: %v", host, err)
				return false
			}
			// label.suffix is the registrable domain, so it maps to itself.
			if got != host {
				t.Logf("host %q: expected eTLD+1 %q, got %q", host, host, got)
				return false
			}
			return true
		},
		regLabelGen,
		suffixGen,
	))

	// Property 1b: sub.label.<suffix> normalizes to label.suffix — the subdomain
	// input's WHOIS query target equals its eTLD+1, so subdomains are never treated
	// as independent monitoring targets (requirements 4.2 / 4.8).
	properties.Property("a subdomain input normalizes to its registrable eTLD+1", prop.ForAll(
		func(sub, label, suffix string) bool {
			root := label + "." + suffix
			host := sub + "." + root
			got, err := RegistrableDomain(host)
			if err != nil {
				t.Logf("host %q: unexpected error: %v", host, err)
				return false
			}
			if got != root {
				t.Logf("host %q: expected registrable target %q, got %q", host, root, got)
				return false
			}
			// The subdomain's query target must equal the bare root's eTLD+1.
			rootGot, rootErr := RegistrableDomain(root)
			if rootErr != nil {
				t.Logf("root %q: unexpected error: %v", root, rootErr)
				return false
			}
			if got != rootGot {
				t.Logf("subdomain target %q != root eTLD+1 %q", got, rootGot)
				return false
			}
			return true
		},
		subLabelGen,
		regLabelGen,
		suffixGen,
	))

	// Property 1c: a public suffix itself is not a registrable domain -> error.
	properties.Property("a public suffix itself is rejected as ErrInvalidDomain", prop.ForAll(
		func(suffix string) bool {
			got, err := RegistrableDomain(suffix)
			if err == nil {
				t.Logf("public suffix %q: expected error, got result %q", suffix, got)
				return false
			}
			if !errors.Is(err, ErrInvalidDomain) {
				t.Logf("public suffix %q: expected ErrInvalidDomain, got %v", suffix, err)
				return false
			}
			return true
		},
		suffixGen,
	))

	// Property 1d: syntactically invalid input is rejected. Each generated category
	// deterministically has no eTLD+1:
	//   - whitespace-only / empty  -> normalizes to "" -> ErrInvalidDomain
	//   - single label, no dot     -> a bare label has no eTLD+1
	//   - leading dot              -> empty leading label
	//   - consecutive dots         -> empty interior label
	//   - embedded space, no dot   -> a single (space-containing) label, no eTLD+1
	invalidGen := gen.OneGenOf(
		gen.OneConstOf("", " ", "   ", "\t", "  \t "),
		gen.RegexMatch("[a-z0-9]{1,12}"),
		gen.RegexMatch("[a-z0-9]{1,8}").Map(func(s string) string { return "." + s }),
		gen.RegexMatch("[a-z0-9]{1,6}").Map(func(s string) string { return s + ".." + s }),
		gen.RegexMatch("[a-z0-9]{1,6}").Map(func(s string) string { return s + " " + s }),
	)
	properties.Property("syntactically invalid input is rejected as ErrInvalidDomain", prop.ForAll(
		func(bad string) bool {
			got, err := RegistrableDomain(bad)
			if err == nil {
				t.Logf("invalid input %q: expected error, got result %q", bad, got)
				return false
			}
			if !errors.Is(err, ErrInvalidDomain) {
				t.Logf("invalid input %q: expected ErrInvalidDomain, got %v", bad, err)
				return false
			}
			return true
		},
		invalidGen,
	))

	// Property 1e: RFC/DNS-syntax-invalid inputs are rejected as ErrInvalidDomain
	// by the server-side DNS-name validation performed BEFORE the Public Suffix
	// computation. Without that validation publicsuffix.EffectiveTLDPlusOne can
	// still return an "eTLD+1" for these, letting invalid monitoring targets be
	// registered. Covers every category called out by the review finding:
	//   - underscore in a label        (foo_bar.com)        — not a valid LDH char
	//   - leading hyphen               (-bad.com)
	//   - trailing hyphen              (bad-.com)
	//   - over-63-char label           (generated 64+ 'a's + .com)
	//   - over-253-char full name      (304-char name below)
	//   - IPv4 literal                 (1.2.3.4)
	//   - IPv6 literal                 (2001:db8::1, ::1)
	//   - URL scheme                   (http://example.com)
	//   - host:port                    (example.com:8080)
	// None of these is a valid registrable domain.
	//
	// **Validates: Requirements 3.3, 4.2**
	//
	// A 304-char full name whose only defect is exceeding the 253-char total limit
	// (each of the 5 labels is 60 'a's, i.e. <= 63, so labels themselves are legal).
	overLong253 := strings.Join([]string{
		strings.Repeat("a", 60),
		strings.Repeat("a", 60),
		strings.Repeat("a", 60),
		strings.Repeat("a", 60),
		strings.Repeat("a", 60),
	}, ".")
	curatedInvalidGen := gen.OneConstOf(
		"foo_bar.com",        // underscore is not a valid LDH character
		"-bad.com",           // leading hyphen in a label
		"bad-.com",           // trailing hyphen in a label
		"1.2.3.4",            // IPv4 literal is not a registrable domain
		"2001:db8::1",        // IPv6 literal
		"::1",                // IPv6 loopback literal
		"http://example.com", // carries a URL scheme
		"example.com:8080",   // carries a host:port
		overLong253,          // 304-char full name exceeds the 253-char limit
	)
	// A generated label of 64-80 'a's under .com exceeds the 63-char per-label limit.
	overLongLabelGen := gen.RegexMatch("a{64,80}").Map(func(label string) string {
		return label + ".com"
	})
	syntaxInvalidGen := gen.OneGenOf(curatedInvalidGen, overLongLabelGen)
	properties.Property("syntactically invalid inputs (bad charset/hyphen/over-long/IP/scheme/port) are rejected as ErrInvalidDomain", prop.ForAll(
		func(bad string) bool {
			got, err := RegistrableDomain(bad)
			if err == nil {
				t.Logf("invalid input %q: expected error, got result %q", bad, got)
				return false
			}
			if !errors.Is(err, ErrInvalidDomain) {
				t.Logf("invalid input %q: expected ErrInvalidDomain, got %v", bad, err)
				return false
			}
			return true
		},
		syntaxInvalidGen,
	))

	properties.TestingRun(t)
}
