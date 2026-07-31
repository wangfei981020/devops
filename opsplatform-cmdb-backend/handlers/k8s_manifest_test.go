package handlers

import "testing"

// 脱敏是 get_manifest 的安全边界，且有两个方向的失败都会造成实际损失：
//   - 漏脱：密码明文进了 AI 上下文
//   - 过脱：secretName 这类引用名被打码，排障时查不到到底引用了哪个 Secret，链路直接断掉
//
// 所以两个方向都要钉住。
func TestLooksSensitiveKey(t *testing.T) {
	sensitive := []string{
		"password", "PASSWORD", "passwd", "DB_PASSWORD", "mysql_password",
		"token", "ACCESS_TOKEN", "apikey", "API_KEY_VALUE",
		"credential", "privatekey", "keystorePassword", "passphrase", "dsn",
	}
	for _, k := range sensitive {
		if !looksSensitiveKey(k) {
			t.Errorf("应判为敏感但没判出来: %q", k)
		}
	}

	// 引用名必须原样保留：排障靠它们定位到底引用了哪个对象
	references := []string{
		"secretName", "secretKeyRef", "name", "configMapRef", "configMapKeyRef",
		"tokenPath", "credentialsPath", "imagePullSecretName", "serviceAccountName",
	}
	for _, k := range references {
		if looksSensitiveKey(k) {
			t.Errorf("引用名被误判为敏感值（会导致排障断链）: %q", k)
		}
	}

	// 普通字段不该被碰
	for _, k := range []string{"image", "replicas", "path", "port", "level"} {
		if looksSensitiveKey(k) {
			t.Errorf("普通字段被误脱敏: %q", k)
		}
	}
}

// env 数组是最典型的形态：敏感信息在 name 里，要脱的是 value。
func TestRedactAnyEnvStyle(t *testing.T) {
	obj := map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{
					"name": "app",
					"env": []any{
						map[string]any{"name": "DB_PASSWORD", "value": "hunter2"},
						map[string]any{"name": "LOG_LEVEL", "value": "debug"},
						// 引用型 env：value 不存在，valueFrom 必须完整保留
						map[string]any{"name": "API_TOKEN", "valueFrom": map[string]any{
							"secretKeyRef": map[string]any{"name": "app-secret", "key": "token"},
						}},
					},
				},
			},
		},
	}
	n := redactAny(obj)
	if n == 0 {
		t.Fatal("一处都没脱敏，DB_PASSWORD 的值泄漏了")
	}

	envs := obj["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)["env"].([]any)
	if got := envs[0].(map[string]any)["value"]; got != redactedMark {
		t.Errorf("DB_PASSWORD 未脱敏，得到 %v", got)
	}
	if got := envs[1].(map[string]any)["value"]; got != "debug" {
		t.Errorf("非敏感 env 被误脱敏: %v", got)
	}
	// secretKeyRef 里的 name/key 是引用坐标，脱了就没法查到底引用了什么
	ref := envs[2].(map[string]any)["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]any)
	if ref["name"] != "app-secret" || ref["key"] != "token" {
		t.Errorf("secretKeyRef 引用坐标被改动: %v", ref)
	}
}

func TestScrubManifestDropsNoise(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{
			"name":          "web",
			"managedFields": []any{map[string]any{"manager": "kubectl"}},
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": `{"spec":{"env":[{"name":"PASSWORD","value":"leak"}]}}`,
				"deployment.kubernetes.io/revision":                "3",
			},
		},
		"spec": map[string]any{"password": "topsecret"},
	}
	scrubManifest(obj)

	md := obj["metadata"].(map[string]any)
	if _, ok := md["managedFields"]; ok {
		t.Error("managedFields 未被移除")
	}
	ann := md["annotations"].(map[string]any)
	if _, ok := ann["kubectl.kubernetes.io/last-applied-configuration"]; ok {
		// 这条注解是整份 spec 的副本，留着等于把 env 明文再抄一遍
		t.Error("last-applied-configuration 未被移除，env 明文会从这里泄漏")
	}
	if ann["deployment.kubernetes.io/revision"] != "3" {
		t.Error("正常注解被误删")
	}
	if got := obj["spec"].(map[string]any)["password"]; got != redactedMark {
		t.Errorf("spec.password 未脱敏，得到 %v", got)
	}
}

// 空注解 map 应该整个删掉，不要在 YAML 里留一个空壳干扰阅读。
func TestScrubManifestRemovesEmptyAnnotations(t *testing.T) {
	obj := map[string]any{
		"metadata": map[string]any{
			"annotations": map[string]any{
				"kubectl.kubernetes.io/last-applied-configuration": "{}",
			},
		},
	}
	scrubManifest(obj)
	if _, ok := obj["metadata"].(map[string]any)["annotations"]; ok {
		t.Error("清空后的 annotations 应被删除")
	}
}
