// Package evidence 是审计的防篡改与取证导出。
//
// # 要解决的问题
//
// 审计日志的价值取决于「它没被改过」这个前提。但审计表和业务表在同一个库里，
// 有 DBA 权限的人（也包括我们自己的工程师）可以 UPDATE 任何一行。
// 客户的安全评审一定会问这个问题。
//
// # 做法：哈希链
//
// 每条记录算一个哈希，且**把上一条的哈希算进去**：
//
//	h(n) = SHA256( h(n-1) || 这条记录的规范化内容 )
//
// 于是改动第 n 条，从第 n 条起后面所有哈希都对不上 —— 除非把整条链全部重算，
// 而重算需要知道每条记录的原文，且会留下"全表被重写"这个明显痕迹。
//
// # 它防得住什么、防不住什么（必须说清楚）
//
//	防得住：改一条、删一条、插一条 —— 都会让链断在那里
//	防不住：把整条链从某点起全部重算。要防这个需要**外部锚点** ——
//	        定期把链头哈希用 Ed25519 签名并送到系统之外（对象存储/邮件/第三方）。
//	        锚点之前的部分就再也改不动了。
//
// 把"防不住什么"写在这里，是因为对客户讲不清楚边界的安全机制，
// 第一次真出事时会变成信任危机。
package evidence

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// GenesisHash 链的起点。第一条记录的 prev 用它。
const GenesisHash = "0000000000000000000000000000000000000000000000000000000000000000"

var (
	ErrChainBroken = errors.New("evidence: 哈希链断裂")
	ErrBadAnchor   = errors.New("evidence: 锚点签名校验失败")
)

// Entry 参与哈希链的一条记录。
//
// Fields 是这条记录的业务内容。用 map 而不是固定结构，是因为访问事件与
// 配置变更的字段不一样，但它们要串在同一条链上 —— 两条链等于两份可信度。
type Entry struct {
	Seq      int64
	At       time.Time
	Fields   map[string]any
	PrevHash string
	Hash     string
}

// Canonical 把一条记录序列化成**稳定**的字节串。
//
// 稳定性是哈希链的全部前提：同一条记录在任何机器、任何 Go 版本、
// 任何 map 遍历顺序下都必须得到同一份字节。所以这里手工按 key 排序拼接，
// 不用 json.Marshal(map) —— Go 的 map 序列化虽然目前有序，但那是实现细节，
// 把安全属性押在实现细节上是很糟的赌注。
func Canonical(seq int64, at time.Time, fields map[string]any) []byte {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, "seq=%d\n", seq)
	// 用 UTC 纳秒：时区在导出/导入时会变，纳秒在不同存储精度下会被截断，
	// 所以统一到 UnixNano 再格式化成十进制，不留任何解释空间
	fmt.Fprintf(&b, "at=%d\n", at.UTC().UnixNano())
	for _, k := range keys {
		v, _ := json.Marshal(fields[k])
		fmt.Fprintf(&b, "%s=%s\n", k, v)
	}
	return []byte(b.String())
}

// CanonicalJSON 把任意 JSON 值序列化成**稳定**字符串：键排序、无空格、递归。
//
// # 为什么不能直接哈希原始 JSON 字节
//
// 因为字节根本不归我们管。detail 存在 MySQL 的 JSON 列里，而 JSON 列
// 存的是二进制规范形式 —— 读回来的键顺序是「按键长度、再按字典序」，
// 分隔符也变成 `": "`，和 Go 的 json.Marshal（纯字典序、无空格）不一样。
// 于是「写进去的字节」和「读回来的字节」天生不同，
// **每一条带 detail 的审计都会被报成「内容被改过」**。
// 实测就是这样：链上第一条没有 detail 所以过了，第二条一带 detail 就断。
//
// 一个天天喊狼来了的防篡改机制比没有还糟：人会直接不看它，
// 于是真出事的那次也不会有人看。
//
// 正确的做法是**两侧都对解析后的结构做规范化**，而不是信任字节。
// 这样中间过多少层（MySQL JSON 列、导出再导入、跨语言实现）都不影响结论。
//
// ⚠️ 数字统一走 float64（JSON 本来就没有整数类型）。超过 2^53 的整数会丢精度，
// 但两侧丢得一模一样，所以不影响比对；真要放大整数，请存成字符串。
func CanonicalJSON(v any) string {
	var b strings.Builder
	writeCanonical(&b, v)
	return b.String()
}

func writeCanonical(b *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			writeCanonical(b, t[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonical(b, e)
		}
		b.WriteByte(']')
	default:
		// 标量：交给 encoding/json，它对 string/bool/null/数字的输出是确定的。
		// 但**必须先把所有数字统一成 float64** —— 写入侧拿到的是 Go 的 int64，
		// 读回侧从 JSON 解析出来的是 float64，不统一的话 4 会写成 "4" 和 "4"
		// 看着一样，而 1e21 这类会一个写成 1000000000000000000000 一个写成 1e+21。
		// ★ 结构体/具名 map/切片必须先降解成 map[string]any 再规范化。
		//
		// 不降解的话，json.Marshal 会按**结构体字段声明顺序**输出，
		// 而校验时这份 detail 是从库里解析回来的 map，按字典序输出 ——
		// 两边字节不同，于是这条记录被报成「内容被改过」。
		//
		// 实测撞过：删应用的审计里带了一个 AppDeps 结构体，
		// 写进去那一刻链就断在那一条，而记录本身谁也没动过。
		// 上一次同类问题（MySQL JSON 列重排键）已经修过一遍，
		// 这次是同一个陷阱的另一个入口，所以修在这里 —— 让调用方
		// 传什么类型都不影响结论，而不是要求每个调用方记得传 map。
		if d, ok := degradeStruct(v); ok {
			writeCanonical(b, d)
			return
		}
		out, _ := json.Marshal(toFloat(v))
		b.Write(out)
	}
}

// degrade 把任意值过一遍 JSON，降解成 map[string]any / []any / 标量。
func degrade(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

// degradeStruct 只对「JSON 出来是对象或数组」的值降解。
//
// 标量不走这条路：标量已经由 toFloat + json.Marshal 处理得很确定，
// 多绕一圈只会引入新的不确定性（比如自定义 MarshalJSON 的类型）。
func degradeStruct(v any) (any, bool) {
	switch degraded := degrade(v).(type) {
	case map[string]any:
		return degraded, true
	case []any:
		return degraded, true
	}
	return nil, false
}

func toFloat(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int32:
		return float64(n)
	case int64:
		return float64(n)
	case uint:
		return float64(n)
	case uint32:
		return float64(n)
	case uint64:
		return float64(n)
	case float32:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return v
	}
}

// CanonicalJSONString 把一段 JSON 文本规范化。解析不了就原样返回 ——
// 这时候两侧至少还能按字节比，不至于凭空断链。
func CanonicalJSONString(s string) string {
	if s == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s
	}
	return CanonicalJSON(v)
}

// Next 算出链上下一条的哈希。
func Next(prevHash string, seq int64, at time.Time, fields map[string]any) string {
	h := sha256.New()
	h.Write([]byte(prevHash))
	h.Write(Canonical(seq, at, fields))
	return hex.EncodeToString(h.Sum(nil))
}

// Append 把一条记录挂到链上。
func Append(prev Entry, seq int64, at time.Time, fields map[string]any) Entry {
	prevHash := prev.Hash
	if prevHash == "" {
		prevHash = GenesisHash
	}
	return Entry{
		Seq: seq, At: at, Fields: fields,
		PrevHash: prevHash,
		Hash:     Next(prevHash, seq, at, fields),
	}
}

// VerifyResult 校验结论。
//
// 失败时给出**具体断在哪一条**，而不是笼统的"校验失败"——
// 后者除了让人心慌之外没有任何用处。
type VerifyResult struct {
	OK       bool
	Checked  int
	BrokenAt int64  // 第一条对不上的 seq
	Reason   string // 原因码，不是中文
}

// Verify 校验一段链。entries 必须按 seq 升序。
func Verify(entries []Entry) VerifyResult {
	if len(entries) == 0 {
		return VerifyResult{OK: true}
	}
	prev := entries[0].PrevHash
	for i, e := range entries {
		if e.PrevHash != prev {
			return VerifyResult{OK: false, Checked: i, BrokenAt: e.Seq, Reason: "prev_hash_mismatch"}
		}
		want := Next(e.PrevHash, e.Seq, e.At, e.Fields)
		if want != e.Hash {
			// 记录内容被改过：存的哈希与按内容重算出来的对不上
			return VerifyResult{OK: false, Checked: i, BrokenAt: e.Seq, Reason: "content_tampered"}
		}
		if i > 0 && e.Seq <= entries[i-1].Seq {
			// 序号回退或重复 —— 插入行为的典型痕迹
			return VerifyResult{OK: false, Checked: i, BrokenAt: e.Seq, Reason: "seq_out_of_order"}
		}
		prev = e.Hash
	}
	return VerifyResult{OK: true, Checked: len(entries)}
}

// ── 外部锚点 ────────────────────────────────────────────────────

// Anchor 一个把链头钉死在时间上的签名。
type Anchor struct {
	Seq       int64     `json:"seq"`
	Hash      string    `json:"hash"`
	CreatedAt time.Time `json:"created_at"`
	Signature string    `json:"signature"` // Ed25519(hex)
	PublicKey string    `json:"public_key"`
}

// Sign 用私钥给链头签名。
//
// 签名内容只有 seq + hash + 时间：越少越好，越少越不会因为字段增删而失效。
func Sign(priv ed25519.PrivateKey, seq int64, hash string, at time.Time) Anchor {
	msg := anchorMessage(seq, hash, at)
	sig := ed25519.Sign(priv, msg)
	return Anchor{
		Seq: seq, Hash: hash, CreatedAt: at,
		Signature: hex.EncodeToString(sig),
		PublicKey: hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
	}
}

// VerifyAnchor 校验锚点签名。
//
// 第三方拿着公钥就能离线验 —— **不需要信任我们的系统**，
// 这正是它能出现在合规材料里的原因。
func VerifyAnchor(a Anchor) error {
	pub, err := hex.DecodeString(a.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: 公钥格式不对", ErrBadAnchor)
	}
	sig, err := hex.DecodeString(a.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: 签名格式不对", ErrBadAnchor)
	}
	if !ed25519.Verify(pub, anchorMessage(a.Seq, a.Hash, a.CreatedAt), sig) {
		return ErrBadAnchor
	}
	return nil
}

func anchorMessage(seq int64, hash string, at time.Time) []byte {
	return []byte(fmt.Sprintf("oap-anchor/v1|%d|%s|%d", seq, hash, at.UTC().Unix()))
}

// ── 取证包 ──────────────────────────────────────────────────────

// Export 一个可离线校验的取证包。
type Export struct {
	Product   string    `json:"product"`
	Version   string    `json:"version"`
	Tenant    int64     `json:"tenant_id"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Entries   []Entry   `json:"entries"`
	Anchors   []Anchor  `json:"anchors"`
	Redacted  []string  `json:"redacted_fields"`
	CreatedAt time.Time `json:"created_at"`
}

// 默认脱敏字段。导出物会被邮件转发、被贴进工单、被截图 ——
// 这里漏一个字段，等于把凭据发给了所有看得到这份文件的人。
var defaultRedact = []string{
	"password", "secret", "token", "code", "credential", "private_key", "webhook",
}

// Redact 按字段名脱敏。
//
// 匹配是**子串不区分大小写**：宁可多脱一个（把 token_count 也脱了），
// 也不要漏一个（把 refreshToken 放出去）。
func Redact(fields map[string]any, extra []string) (map[string]any, []string) {
	rules := append(append([]string{}, defaultRedact...), extra...)
	out := make(map[string]any, len(fields))
	var hit []string
	for k, v := range fields {
		lower := strings.ToLower(k)
		var masked bool
		for _, r := range rules {
			if strings.Contains(lower, strings.ToLower(r)) {
				masked = true
				break
			}
		}
		if masked {
			out[k] = "***"
			hit = append(hit, k)
		} else {
			out[k] = v
		}
	}
	sort.Strings(hit)
	return out, hit
}

// ⚠️ 脱敏会改变记录内容，因此**脱敏后的记录哈希必然对不上**。
// 取证包里同时保留原始哈希与脱敏标记，让第三方能分辨：
// 「这条对不上是因为脱敏」还是「这条被人改过」。
// 混为一谈的话，任何一次导出都会让整条链看起来是断的。
func (e Export) VerifiableEntries() []Entry {
	var out []Entry
	for _, en := range e.Entries {
		if len(e.Redacted) == 0 {
			out = append(out, en)
		}
	}
	return out
}
