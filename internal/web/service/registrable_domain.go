package service

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// ErrInvalidDomain 表示输入的域名语法非法，或其本身是一个公共后缀
// （如 com.cn / co.uk），无法作为可注册域名（eTLD+1）监控。
//
// 该哨兵错误在本文件统一定义，供本功能各层（如手动添加校验、WHOIS 客户端）
// 通过 errors.Is 判定；请勿在其它文件重复定义。
var ErrInvalidDomain = errors.New("invalid domain")

// normalizeDomain 规范化主机名：去除首尾空白、转为小写、去除尾部的 "."。
// 例如：" Example.COM. " → "example.com"。
func normalizeDomain(host string) string {
	h := strings.TrimSpace(host)
	h = strings.ToLower(h)
	h = strings.TrimSuffix(h, ".")
	return h
}

// validateDomainSyntax 校验（已由 normalizeDomain 归一化后的）主机名是否符合
// RFC 意义上的 DNS 名称语法。输入 h 已被去空白、转小写、去尾部点。
//
// 校验规则（任何一项不满足都返回包裹 ErrInvalidDomain 的错误）：
//   - 非空；
//   - 总长度 <= 253；
//   - 不是 IP 字面量（拒绝 IPv4/IPv6，它们不是可注册域名）；
//   - 至少包含一个 "."（可注册域名需 >= 2 个 label）；
//   - 每个 label 均通过 validateLabel。
//
// 该校验在 publicsuffix 计算之前执行，用于挡住 foo_bar.com、-bad.com、
// bad-.com、超长 label、IP 地址、带 scheme/端口的字符串等——这些输入
// publicsuffix.EffectiveTLDPlusOne 可能仍会算出一个 “eTLD+1” 而被误接受。
//
// IDN 说明：本校验刻意只接受 ASCII 的 letter-digit-hyphen（LDH）字符集；
// 国际化域名（IDN，UTS #46 / IDNA 的 Unicode↔punycode 归一化）不在此处理，
// 属超出范围的能力。已是 punycode 形式的 "xn--" 标签属于合法 LDH，可通过校验。
func validateDomainSyntax(h string) error {
	if h == "" {
		return ErrInvalidDomain
	}
	if len(h) > 253 {
		return fmt.Errorf("%w: total length %d exceeds 253", ErrInvalidDomain, len(h))
	}
	// 拒绝 IP 字面量（IPv4/IPv6）。放在 "." 校验之前，使 1.2.3.4 与 ::1 都被拦下。
	if net.ParseIP(h) != nil {
		return fmt.Errorf("%w: %q is an IP address", ErrInvalidDomain, h)
	}
	// 可注册域名至少需要两个 label（如 example.com），故必须包含 "."。
	if !strings.Contains(h, ".") {
		return fmt.Errorf("%w: %q has no dot (needs at least two labels)", ErrInvalidDomain, h)
	}
	for _, label := range strings.Split(h, ".") {
		if err := validateLabel(label); err != nil {
			return err
		}
	}
	return nil
}

// validateLabel 校验单个 DNS label（点分隔的一段）。label 必须：
//   - 非空（借此拒绝首/尾/连续的点）；
//   - 长度 <= 63；
//   - 首尾字符都不是连字符 "-"；
//   - 仅由 a-z、0-9、"-" 组成（输入已为小写）。
//
// 任何其它字符（含 "_"、空格、":"、"/"）都会使该 label 非法。
func validateLabel(label string) error {
	if label == "" {
		return fmt.Errorf("%w: empty label", ErrInvalidDomain)
	}
	if len(label) > 63 {
		return fmt.Errorf("%w: label %q exceeds 63 characters", ErrInvalidDomain, label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("%w: label %q has a leading or trailing hyphen", ErrInvalidDomain, label)
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
			return fmt.Errorf("%w: label %q contains invalid character %q", ErrInvalidDomain, label, string(c))
		}
	}
	return nil
}

// RegistrableDomain 计算 host 的可注册域名（eTLD+1，即根域名）。
//
//   - 先对（经 normalizeDomain 归一化后的）主机名执行服务端 DNS 名称语法校验
//     （见 validateDomainSyntax：label 长度、总长度、字符集 a-z/0-9/-、
//     首尾连字符，拒绝 IP 字面量、带 scheme/端口的字符串等），再依据公共后缀
//     列表（Public Suffix List）计算 eTLD+1。前端正则不能替代该后端校验。
//   - 依据公共后缀列表正确处理多级公共后缀（如 com.cn / co.uk / com.au），
//     使 example.com.cn、example.co.uk 被识别为可注册根域名。
//   - 若 host 语法非法（如 foo_bar.com、-bad.com、1.2.3.4、http://example.com），
//     或其本身是公共后缀（如 com.cn），返回包裹了 ErrInvalidDomain 的错误。
//
// IDN 说明：仅接受 ASCII 的 letter-digit-hyphen（LDH）标签；国际化域名
// （IDN，UTS #46 / IDNA 归一化）刻意不在此处理。punycode 形式的 "xn--"
// 标签属于合法 LDH，可通过校验。
//
// 对子域名输入（如 www.example.com），返回其 eTLD+1（example.com），
// 从而保证只将根域名作为监控对象。
func RegistrableDomain(host string) (string, error) {
	h := normalizeDomain(host)
	if h == "" {
		return "", ErrInvalidDomain
	}
	if err := validateDomainSyntax(h); err != nil {
		return "", err
	}
	etld1, err := publicsuffix.EffectiveTLDPlusOne(h)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidDomain, err)
	}
	return etld1, nil
}
