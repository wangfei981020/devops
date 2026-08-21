# OIDC 端到端联调脚本

两个脚本，对着**跑起来的实例**走完整条授权码流程。不是单测 ——
单测证明不了「下游真能接进来」，而那正是 OIDC 唯一重要的事。

## 为什么不用界面点

浏览器只能看到"跳过去又跳回来"，看不出：

- `id_token` 的签名是否真能用 `jwks_uri` 里的公钥验过
- `iss` / `aud` / `nonce` 对不对
- 授权码是不是一次性

这几项错了，界面上一切正常，而下游在接入当天才会发现。

## 跑法

```sh
# 1. 拿一个管理员会话，建一个测试客户端
TOK=$(curl -s -i -X POST http://localhost:30834/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<口令>"}' \
  | sed -n 's/.*oag_session=\([^;]*\).*/\1/p' | tr -d '\r')
echo "$TOK" > /tmp/tok.txt

curl -s -X POST http://localhost:30834/api/v1/oidc-clients \
  -H 'Content-Type: application/json' -H "Cookie: oag_session=$TOK" \
  -d '{"app_id":1,"client_id":"e2e-test","redirect_uris":["http://127.0.0.1:9999/cb"],"public_client":false}' \
  | python3 -c 'import json,sys;open("/tmp/oidc_secret.txt","w").write(json.load(sys.stdin)["client_secret"])'

# 2. 正常流程
python3 flow.py

# 3. 安全路径
python3 security.py
```

## 2026-08-10 首次通电的结果

`flow.py`：authorize 302 带 code → 换 token → **id_token 用 jwks 公钥验签通过**
→ iss/aud/nonce/sid 全对 → userinfo 可用 → code 复用被拒。

`security.py` 六条全部按预期拒绝：

| 场景 | 结果 |
|---|---|
| redirect_uri 不在白名单 | 400，**不跳转**（跳过去就是把 code 送给攻击者） |
| client_id 不存在 | 400，不跳转 |
| 未登录访问 authorize | 302 到登录页并带 next |
| client_secret 错误 | 401 invalid_client |
| PKCE verifier 不匹配 | 400 invalid_grant |
| 换 token 时 redirect_uri 与授权时不一致 | 400 invalid_grant |

⚠️ 脚本会真的建客户端、真的签发 token。**只对本地/测试实例跑**。
