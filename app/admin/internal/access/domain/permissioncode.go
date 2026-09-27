package domain

import (
	"fmt"
	"strings"
)

// PermissionCode 是权限码值对象，形如 domain:resource:action。
//
// 提升为值对象而不是继续用裸 string 的理由：这个字符串同时出现在
// proto 注解、Casbin 策略、权限树三处，任何一处格式走样都会造成
// 「配置看起来成功了但永远不生效」的静默失败。把格式规则和解析逻辑
// 收敛到一个类型上，规则只有一份，越界的值构造不出来。
//
// 值对象的两条性质在这里都成立：不可变（无 setter），
// 相等性由值决定（可直接用 == 比较）。
type PermissionCode struct {
	value string
}

// 权限码的段数与通配符。
const (
	permCodeSep       = ":"
	permCodeWildcard  = "*"
	permCodeSegments  = 3
	maxPermCodeLen    = 128
	maxPermSegmentLen = 64
)

// NewPermissionCode 校验并构造权限码。
//
// 两种合法形态：
//   - 具体权限码：严格三段 domain:resource:action，不含通配
//   - 通配策略：末段为 *，如 system:* 或 system:user:*
//
// 空串也是合法的：目录和菜单节点本就不需要权限码，
// 用零值表示比用 error 表示更贴合领域语义。
//
// 为什么通配只允许在末段：Casbin 的 keyMatch 只看第一个 * 并做前缀匹配，
// 完全忽略其后的内容。也就是说 system:*:add 在 Casbin 眼里等价于
// system:*，会连 system:user:edit 一起放行——写的人以为限定了动作，
// 实际授出了整个域。禁掉这种形态，领域层的 Covers 与 Casbin 的判定
// 才能保证一致，也消除了这个静默扩权的陷阱。
func NewPermissionCode(s string) (PermissionCode, error) {
	if s == "" {
		return PermissionCode{}, nil
	}
	if len(s) > maxPermCodeLen {
		return PermissionCode{}, fmt.Errorf(
			"%w: 权限码长度 %d 超过上限 %d", ErrInvalidPermissionCode, len(s), maxPermCodeLen)
	}

	segments := strings.Split(s, permCodeSep)
	if len(segments) < 2 {
		return PermissionCode{}, fmt.Errorf(
			"%w: %q 至少要有两段", ErrInvalidPermissionCode, s)
	}

	for i, seg := range segments {
		if seg == "" {
			return PermissionCode{}, fmt.Errorf(
				"%w: %q 第 %d 段为空", ErrInvalidPermissionCode, s, i+1)
		}
		if len(seg) > maxPermSegmentLen {
			return PermissionCode{}, fmt.Errorf(
				"%w: %q 第 %d 段长度超过上限 %d", ErrInvalidPermissionCode, s, i+1, maxPermSegmentLen)
		}
		if !strings.Contains(seg, permCodeWildcard) {
			if !validPermissionSegment(seg) {
				return PermissionCode{}, fmt.Errorf(
					"%w: %q 第 %d 段只能包含英文字母和数字且必须以字母开头", ErrInvalidPermissionCode, s, i+1)
			}
			continue
		}
		// 通配必须独占整段，不允许 sys*:user:add 这种半通配
		if seg != permCodeWildcard {
			return PermissionCode{}, fmt.Errorf(
				"%w: %q 的通配符必须独占一段", ErrInvalidPermissionCode, s)
		}
		// 通配必须是最后一段
		if i != len(segments)-1 {
			return PermissionCode{}, fmt.Errorf(
				"%w: %q 的通配符只能出现在末段（Casbin 的 keyMatch 会忽略其后的内容，"+
					"写成 %s 会意外放行整个前缀）", ErrInvalidPermissionCode, s, s)
		}
	}

	// 非通配的权限码必须是严格三段，否则鉴权注解与权限树对不上
	if !strings.HasSuffix(s, permCodeSep+permCodeWildcard) && !strings.Contains(s, permCodeWildcard) {
		if len(segments) != permCodeSegments {
			return PermissionCode{}, fmt.Errorf(
				"%w: %q 应为 domain:resource:action 三段", ErrInvalidPermissionCode, s)
		}
	}

	return PermissionCode{value: s}, nil
}

func validPermissionSegment(segment string) bool {
	for i, r := range segment {
		if i == 0 && !isASCIILetter(r) {
			return false
		}
		if !isASCIILetter(r) && (r < '0' || r > '9') {
			return false
		}
	}
	return segment != ""
}

func isASCIILetter(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

// MustPermissionCode 在构造失败时 panic，仅供测试与常量声明使用。
func MustPermissionCode(s string) PermissionCode {
	c, err := NewPermissionCode(s)
	if err != nil {
		panic(err)
	}
	return c
}

// String 返回权限码的字符串形式。
func (c PermissionCode) String() string { return c.value }

// IsZero 表示这是目录/菜单这类无需权限码的节点。
func (c PermissionCode) IsZero() bool { return c.value == "" }

// Domain 返回第一段（业务域），零值返回空串。
func (c PermissionCode) Domain() string { return c.segment(0) }

// Resource 返回第二段（资源）。
func (c PermissionCode) Resource() string { return c.segment(1) }

// Action 返回第三段（动作）。
func (c PermissionCode) Action() string { return c.segment(2) }

func (c PermissionCode) segment(i int) string {
	if c.IsZero() {
		return ""
	}
	parts := strings.Split(c.value, permCodeSep)
	if i >= len(parts) {
		return ""
	}
	return parts[i]
}

// HasWildcard 表示该权限码含通配段，是一条覆盖性策略而非具体权限。
func (c PermissionCode) HasWildcard() bool {
	return strings.Contains(c.value, permCodeWildcard)
}

// Covers 判断本权限码（作为策略）是否覆盖 target（作为请求）。
//
// 实现必须与 Casbin 模型里的 keyMatch 逐字节一致，否则会出现
// 「后台显示已授权、实际调用 403」，或者更糟——领域层以为没授权、
// Casbin 却放行的越权。
//
// keyMatch 的语义是前缀匹配：截取到第一个 * 之前的部分做比较。
// 由于构造时已强制通配只能在末段，这里的前缀比较与「按段匹配」
// 在结果上等价，且不会出现 system:*:add 那种两边理解不同的情况。
func (c PermissionCode) Covers(target PermissionCode) bool {
	if c.IsZero() || target.IsZero() {
		return false
	}

	i := strings.Index(c.value, permCodeWildcard)
	if i < 0 {
		return c.value == target.value
	}

	prefix := c.value[:i]
	return strings.HasPrefix(target.value, prefix)
}

// ParsePermissionCodes 批量构造，任一失败即整体失败。
// 用于「全量覆盖角色权限」这类要么全成、要么不动的场景。
func ParsePermissionCodes(ss []string) ([]PermissionCode, error) {
	out := make([]PermissionCode, 0, len(ss))
	for _, s := range ss {
		c, err := NewPermissionCode(s)
		if err != nil {
			return nil, err
		}
		if c.IsZero() {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// PermissionCodeStrings 把权限码切片还原为字符串切片，供基础设施层使用。
func PermissionCodeStrings(codes []PermissionCode) []string {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		out = append(out, c.String())
	}
	return out
}
