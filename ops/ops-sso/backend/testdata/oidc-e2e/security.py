"""OIDC 的安全路径。这几条错了就是漏洞，不是体验问题。"""
import base64, hashlib, json, os, urllib.parse, urllib.request, urllib.error
BASE="http://localhost:30834"; CID="e2e-test"; REDIRECT="http://127.0.0.1:9999/cb"
SECRET=open("/tmp/oidc_secret.txt").read().strip(); SESSION=open("/tmp/tok.txt").read().strip()
def b64u(b): return base64.urlsafe_b64encode(b).rstrip(b"=").decode()
class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,*a,**k): return None
op=urllib.request.build_opener(NoRedirect)

def authorize(**over):
    v=b64u(os.urandom(32)); ch=b64u(hashlib.sha256(v.encode()).digest())
    q={"client_id":CID,"redirect_uri":REDIRECT,"response_type":"code","scope":"openid",
       "state":"s1","nonce":"n1","code_challenge":ch,"code_challenge_method":"S256"}
    q.update(over)
    r=urllib.request.Request(f"{BASE}/oidc/authorize?{urllib.parse.urlencode(q)}")
    if over.get("_nosession") is None: r.add_header("Cookie", f"oag_session={SESSION}")
    try:
        resp=op.open(r); return resp.status, resp.headers.get("Location"), resp.read()[:120]
    except urllib.error.HTTPError as e: return e.code, e.headers.get("Location"), e.read()[:160]

def token(**over):
    b={"grant_type":"authorization_code","redirect_uri":REDIRECT,"client_id":CID,
       "client_secret":SECRET}; b.update(over)
    try:
        r=urllib.request.urlopen(urllib.request.Request(f"{BASE}/oidc/token",
            data=urllib.parse.urlencode(b).encode(),
            headers={"Content-Type":"application/x-www-form-urlencoded"}))
        return r.status, r.read()[:120]
    except urllib.error.HTTPError as e: return e.code, e.read()[:160]

print("=== 1. redirect_uri 不在白名单 —— 绝不能跳过去 ===")
st,loc,body=authorize(redirect_uri="https://evil.example.com/steal")
print(f"   HTTP {st}  Location={loc}")
print(f"   {'✓ 没有跳转' if not loc else '❌ 跳到了攻击者地址！'}")

print("=== 2. client_id 不存在 ===")
st,loc,body=authorize(client_id="nope")
print(f"   HTTP {st}  Location={loc}  {'✓ 没跳转' if not loc else '❌ 跳了'}")

print("=== 3. 未登录访问 authorize ===")
st,loc,body=authorize(_nosession=True)
print(f"   HTTP {st}  Location={(loc or '')[:70]}")
print(f"   {'✓ 送去登录页' if loc and '/oidc' not in loc.split('?')[0] else '（看是否为登录跳转）'}")

print("=== 4. 拿一个合法 code，但用错误的 client_secret 换 ===")
v=b64u(os.urandom(32)); ch=b64u(hashlib.sha256(v.encode()).digest())
st,loc,_=authorize(code_challenge=ch)
code=urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)["code"][0]
st,body=token(code=code, code_verifier=v, client_secret="wrong-secret")
print(f"   HTTP {st}  {'✓ 被拒' if st>=400 else '❌ 竟然换到了 token'}  {body[:80]}")

print("=== 5. PKCE：verifier 不匹配 ===")
v2=b64u(os.urandom(32)); ch2=b64u(hashlib.sha256(v2.encode()).digest())
st,loc,_=authorize(code_challenge=ch2)
code2=urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)["code"][0]
st,body=token(code=code2, code_verifier=b64u(os.urandom(32)))
print(f"   HTTP {st}  {'✓ 被拒' if st>=400 else '❌ 校验形同虚设'}  {body[:80]}")

print("=== 6. 换 token 时 redirect_uri 与授权时不一致 ===")
v3=b64u(os.urandom(32)); ch3=b64u(hashlib.sha256(v3.encode()).digest())
st,loc,_=authorize(code_challenge=ch3)
code3=urllib.parse.parse_qs(urllib.parse.urlparse(loc).query)["code"][0]
st,body=token(code=code3, code_verifier=v3, redirect_uri="http://127.0.0.1:9999/other")
print(f"   HTTP {st}  {'✓ 被拒' if st>=400 else '❌ 没有校验一致性'}  {body[:80]}")
