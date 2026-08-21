package evidence

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func chain(n int) []Entry {
	at := time.Unix(1_700_000_000, 0)
	var out []Entry
	var prev Entry
	for i := 1; i <= n; i++ {
		e := Append(prev, int64(i), at.Add(time.Duration(i)*time.Second), map[string]any{
			"action": "policy.create", "actor": "zhangwei", "object_id": i,
		})
		out = append(out, e)
		prev = e
	}
	return out
}

func TestChainVerifies(t *testing.T) {
	got := Verify(chain(50))
	if !got.OK || got.Checked != 50 {
		t.Fatalf("完好的链应通过：%+v", got)
	}
}

func TestFirstEntryUsesGenesis(t *testing.T) {
	c := chain(1)
	if c[0].PrevHash != GenesisHash {
		t.Fatalf("第一条的 prev 应为创世哈希，得到 %s", c[0].PrevHash)
	}
}

// ★ 改一条 → 从那条起断链
func TestTamperedContentDetected(t *testing.T) {
	c := chain(10)
	c[4].Fields["actor"] = "someone-else" // 有人改了第 5 条的操作人

	got := Verify(c)
	if got.OK {
		t.Fatal("改过内容必须被发现")
	}
	if got.BrokenAt != 5 || got.Reason != "content_tampered" {
		t.Fatalf("应指出断在第 5 条且原因是内容被改，得到 %+v", got)
	}
}

// ★ 删一条 → 链断
func TestDeletedEntryDetected(t *testing.T) {
	c := chain(10)
	c = append(c[:4], c[5:]...) // 删掉第 5 条

	got := Verify(c)
	if got.OK {
		t.Fatal("删除必须被发现")
	}
	if got.Reason != "prev_hash_mismatch" {
		t.Fatalf("删除的表现应是 prev 对不上，得到 %+v", got)
	}
}

// ★ 插一条伪造记录 → 链断
func TestInsertedEntryDetected(t *testing.T) {
	c := chain(10)
	fake := Entry{
		Seq: 99, At: time.Unix(1_700_000_100, 0),
		Fields: map[string]any{"action": "policy.delete", "actor": "attacker"},
		// 攻击者不知道该填什么 prev，随便抄了前一条的
		PrevHash: c[3].Hash,
	}
	fake.Hash = Next(fake.PrevHash, fake.Seq, fake.At, fake.Fields)
	c = append(c[:4], append([]Entry{fake}, c[4:]...)...)

	if got := Verify(c); got.OK {
		t.Fatal("插入必须被发现")
	}
}

// 序号回退是插入行为的另一种痕迹
func TestSeqOutOfOrderDetected(t *testing.T) {
	c := chain(5)
	c[3].Seq = 2
	c[3].Hash = Next(c[3].PrevHash, c[3].Seq, c[3].At, c[3].Fields)
	c[4].PrevHash = c[3].Hash
	c[4].Hash = Next(c[4].PrevHash, c[4].Seq, c[4].At, c[4].Fields)

	got := Verify(c)
	if got.OK || got.Reason != "seq_out_of_order" {
		t.Fatalf("序号回退应被发现，得到 %+v", got)
	}
}

// 规范化必须稳定：字段顺序不同、时区不同，哈希必须一致。
// 这是整个机制的前提 —— 不稳定的话，导出到另一台机器上校验就会假报篡改。
func TestCanonicalIsStable(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	a := Next(GenesisHash, 1, at.UTC(), map[string]any{"b": 2, "a": 1, "c": "x"})
	b := Next(GenesisHash, 1, at.In(time.FixedZone("CST", 8*3600)), map[string]any{"c": "x", "a": 1, "b": 2})
	if a != b {
		t.Fatal("字段顺序与时区不同不该改变哈希 —— 否则换台机器校验就会假报篡改")
	}
}

// ── 锚点 ──

func TestAnchorSignAndVerifyOffline(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	_ = pub
	c := chain(20)
	head := c[len(c)-1]

	a := Sign(priv, head.Seq, head.Hash, time.Unix(1_700_009_999, 0))
	if err := VerifyAnchor(a); err != nil {
		t.Fatalf("刚签的锚点应能验过：%v", err)
	}

	// 改链头哈希 → 签名失效
	bad := a
	bad.Hash = "deadbeef"
	if err := VerifyAnchor(bad); err == nil {
		t.Fatal("被改过的锚点必须验不过")
	}

	// 换个公钥 → 验不过（第三方拿错公钥时要能立刻发现）
	otherPub, _, _ := ed25519.GenerateKey(nil)
	bad = a
	bad.PublicKey = hexOf(otherPub)
	if err := VerifyAnchor(bad); err == nil {
		t.Fatal("公钥不匹配必须验不过")
	}
}

func hexOf(b []byte) string {
	const h = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = h[c>>4]
		out[i*2+1] = h[c&0xf]
	}
	return string(out)
}

// ── 脱敏 ──

func TestRedact(t *testing.T) {
	in := map[string]any{
		"actor":         "zhangwei",
		"password":      "hunter2",
		"refreshToken":  "eyJ...",
		"lark_webhook":  "https://open.feishu.cn/hook/xxx",
		"client_secret": "s3cr3t",
		"object_id":     42,
	}
	out, hit := Redact(in, nil)

	for _, k := range []string{"password", "refreshToken", "lark_webhook", "client_secret"} {
		if out[k] != "***" {
			t.Errorf("%s 必须被脱敏，得到 %v", k, out[k])
		}
	}
	if out["actor"] != "zhangwei" || out["object_id"] != 42 {
		t.Error("非敏感字段不该被动")
	}
	if len(hit) != 4 {
		t.Errorf("应报告 4 个被脱敏字段，得到 %v", hit)
	}
}

// 宁可多脱一个也不漏一个：大小写、驼峰、下划线都要覆盖
func TestRedactIsCaseInsensitiveAndSubstring(t *testing.T) {
	out, _ := Redact(map[string]any{
		"API_TOKEN": "x", "userPassword": "y", "SecretKey": "z", "credentialRef": "w",
	}, nil)
	for k, v := range out {
		if v != "***" {
			t.Errorf("%s 应被脱敏（漏一个就等于把凭据发给所有看到这份导出的人）", k)
		}
	}
}

// ★ 存储精度截断会让整条链假报篡改 —— 这是接线时真踩到的坑。
//
// audit_logs.created_at 是 DATETIME（只到秒），而 Canonical 用 UnixNano。
// 写入时按纳秒算哈希、读回来只剩秒，重算必然对不上，于是**每一条**都被
// 报成"内容被改过"。一个天天喊狼来了的防篡改机制，比没有还糟。
//
// 解法是在写入侧把时间截断到存储精度再算哈希。这条用例把它钉住。
func TestStoragePrecisionTruncationBreaksChain(t *testing.T) {
	nano := time.Unix(1_700_000_000, 123_456_789)
	sec := nano.Truncate(time.Second)
	fields := map[string]any{"action": "policy.create"}

	withNano := Next(GenesisHash, 1, nano, fields)
	withSec := Next(GenesisHash, 1, sec, fields)
	if withNano == withSec {
		t.Fatal("纳秒与秒应算出不同哈希 —— 否则这个坑根本不存在，说明 Canonical 没用上时间")
	}

	// 模拟：按纳秒算哈希存进去，读回来时间被截断成秒
	stored := Entry{Seq: 1, At: nano, Fields: fields, PrevHash: GenesisHash, Hash: withNano}
	readBack := stored
	readBack.At = sec

	if got := Verify([]Entry{readBack}); got.OK {
		t.Fatal("按纳秒算、按秒读回，必须报不一致（这正是当初的症状）")
	}

	// 写入侧先截断 → 读回来一致
	truncated := Entry{Seq: 1, At: sec, Fields: fields, PrevHash: GenesisHash, Hash: withSec}
	if got := Verify([]Entry{truncated}); !got.OK {
		t.Fatalf("写入侧截断后应校验通过：%+v", got)
	}
}
