"""最小 RP：走完 OIDC 授权码流程，并对 id_token 做真实验签。

写成脚本而不是点界面，是因为要验的是**协议层**：
浏览器只能看到"跳过去又跳回来"，看不出 id_token 的 iss/aud/nonce 对不对、
签名是不是真能用 jwks 里的公钥验过 —— 而那几项错了，下游会在接入当天才发现。
"""
import base64, hashlib, json, os, urllib.parse, urllib.request, http.cookiejar

BASE = "http://localhost:30834"
CID, REDIRECT = "e2e-test", "http://127.0.0.1:9999/cb"
SECRET = open("/tmp/oidc_secret.txt").read().strip()
SESSION = open("/tmp/tok.txt").read().strip()

def b64u(b): return base64.urlsafe_b64encode(b).rstrip(b"=").decode()

verifier = b64u(os.urandom(32))
challenge = b64u(hashlib.sha256(verifier.encode()).digest())
state, nonce = b64u(os.urandom(9)), b64u(os.urandom(9))

# 1) authorize —— 带着已登录的会话，应当直接 302 回 redirect_uri 并带 code
q = urllib.parse.urlencode({
    "client_id": CID, "redirect_uri": REDIRECT, "response_type": "code",
    "scope": "openid profile email", "state": state, "nonce": nonce,
    "code_challenge": challenge, "code_challenge_method": "S256",
})
req = urllib.request.Request(f"{BASE}/oidc/authorize?{q}")
req.add_header("Cookie", f"oag_session={SESSION}")
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k): return None
op = urllib.request.build_opener(NoRedirect)
try:
    r = op.open(req); loc, code_status = r.headers.get("Location"), r.status
except urllib.error.HTTPError as e:
    loc, code_status = e.headers.get("Location"), e.code
print(f"1) authorize → HTTP {code_status}")
if not loc:
    print("   ❌ 没有 Location 头，流程走不下去"); raise SystemExit(1)
parsed = urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)
print(f"   回跳: {loc.split('?')[0]}")
if "error" in parsed:
    print(f"   ❌ {parsed['error'][0]}: {parsed.get('error_description',[''])[0]}"); raise SystemExit(1)
code = parsed["code"][0]
print(f"   code ✓  state 回传一致: {parsed['state'][0] == state}")

# 2) token —— 用 client_secret + PKCE verifier 换
body = urllib.parse.urlencode({
    "grant_type": "authorization_code", "code": code, "redirect_uri": REDIRECT,
    "client_id": CID, "client_secret": SECRET, "code_verifier": verifier,
}).encode()
try:
    tr = urllib.request.urlopen(urllib.request.Request(f"{BASE}/oidc/token", data=body,
        headers={"Content-Type": "application/x-www-form-urlencoded"}))
    tok = json.load(tr)
except urllib.error.HTTPError as e:
    print(f"2) token → ❌ HTTP {e.code}: {e.read()[:200].decode()}"); raise SystemExit(1)
print(f"2) token → ✓ 拿到 {', '.join(tok.keys())}")
idt = tok["id_token"]

# 3) 验签：用 jwks 里的公钥真验一遍
jwks = json.load(urllib.request.urlopen(f"{BASE}/oidc/jwks"))
h, p, sig = idt.split(".")
def d64(s): return base64.urlsafe_b64decode(s + "=" * (-len(s) % 4))
hdr, claims = json.loads(d64(h)), json.loads(d64(p))
key = next(k for k in jwks["keys"] if k["kid"] == hdr["kid"])
n = int.from_bytes(d64(key["n"]), "big"); e = int.from_bytes(d64(key["e"]), "big")
s_int = int.from_bytes(d64(sig), "big")
em = pow(s_int, e, n).to_bytes((n.bit_length() + 7) // 8, "big")
# PKCS#1 v1.5 + SHA-256 的 DigestInfo 前缀
prefix = bytes.fromhex("3031300d060960864801650304020105000420")
expect = b"\x00\x01" + b"\xff" * (len(em) - 3 - len(prefix) - 32) + b"\x00" + prefix + hashlib.sha256(f"{h}.{p}".encode()).digest()
print(f"3) id_token 验签 → {'✓ 通过' if em == expect else '❌ 不通过'}")

# 4) 声明核对
print("4) 声明:")
for k in ["iss", "aud", "sub", "nonce", "sid", "email", "name"]:
    if k in claims: print(f"     {k} = {claims[k]}")
print(f"   iss 与 discovery 一致: {claims.get('iss') == BASE}")
print(f"   aud 是本客户端      : {claims.get('aud') == CID or CID in (claims.get('aud') or [])}")
print(f"   nonce 原样回传      : {claims.get('nonce') == nonce}")

# 5) userinfo
ur = urllib.request.Request(f"{BASE}/oidc/userinfo")
ur.add_header("Authorization", f"Bearer {tok['access_token']}")
try:
    ui = json.load(urllib.request.urlopen(ur))
    print(f"5) userinfo → ✓ {json.dumps(ui, ensure_ascii=False)[:120]}")
except urllib.error.HTTPError as e:
    print(f"5) userinfo → ❌ HTTP {e.code}: {e.read()[:160].decode()}")

# 6) code 只能用一次
try:
    urllib.request.urlopen(urllib.request.Request(f"{BASE}/oidc/token", data=body,
        headers={"Content-Type": "application/x-www-form-urlencoded"}))
    print("6) code 复用 → ❌ 竟然又成功了，授权码没有一次性")
except urllib.error.HTTPError as e:
    print(f"6) code 复用 → ✓ 被拒 HTTP {e.code}")
