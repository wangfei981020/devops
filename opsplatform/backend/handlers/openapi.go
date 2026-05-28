package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
)

const swaggerSpecPath = "docs/swagger/swagger.json"

var (
	swaggerSpecCache     map[string]interface{}
	swaggerSpecCacheOnce sync.Once
	swaggerSpecCacheErr  error
)

// loadSwaggerSpec 一次性加载并缓存 swag 生成的 spec
func loadSwaggerSpec() (map[string]interface{}, error) {
	swaggerSpecCacheOnce.Do(func() {
		// 寻找 spec 文件：先按相对路径，再按可执行文件所在目录
		paths := []string{
			swaggerSpecPath,
			filepath.Join(getExecDir(), swaggerSpecPath),
		}
		var data []byte
		var err error
		for _, p := range paths {
			data, err = os.ReadFile(p)
			if err == nil {
				break
			}
		}
		if err != nil {
			swaggerSpecCacheErr = err
			return
		}
		var spec map[string]interface{}
		if err := json.Unmarshal(data, &spec); err != nil {
			swaggerSpecCacheErr = err
			return
		}
		swaggerSpecCache = spec
	})
	return swaggerSpecCache, swaggerSpecCacheErr
}

func getExecDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// HandleGetAPISpec 按业务域过滤后的 openapi.json
// @Summary      按业务域返回过滤后的 OpenAPI spec
// @Tags         api_docs
// @Produce      json
// @Param        domain  query     string  true  "业务域 code，如 table_maintenance"
// @Success      200     {object}  map[string]interface{}
// @Failure      400     {object}  ErrorResponse
// @Failure      403     {object}  ErrorResponse
// @Failure      404     {object}  ErrorResponse "spec 文件不存在（构建时未生成）"
// @Router       /api-docs/spec [get]
func HandleGetAPISpec(w http.ResponseWriter, r *http.Request) {
	domain := r.URL.Query().Get("domain")
	if domain == "" {
		sendError(w, "domain 参数必填", http.StatusBadRequest)
		return
	}

	// 校验当前用户对该域有 menu 权限
	if !apiDomainHasMenuPermission(r, domain) {
		sendError(w, "无权查看该业务域文档", http.StatusForbidden)
		return
	}

	spec, err := loadSwaggerSpec()
	if err != nil {
		sendError(w, "OpenAPI spec 未生成或加载失败", http.StatusNotFound)
		return
	}

	// 深拷贝 spec（避免并发污染缓存）
	filtered := deepCloneJSON(spec)

	// 过滤 paths：只保留 tags 包含 domain 的 operation
	canTry := apiDomainCanTryIt(r, domain)
	pathsRaw, ok := filtered["paths"].(map[string]interface{})
	if ok {
		for path, pathItemRaw := range pathsRaw {
			pathItem, ok := pathItemRaw.(map[string]interface{})
			if !ok {
				delete(pathsRaw, path)
				continue
			}
			keptMethods := 0
			for method, opRaw := range pathItem {
				op, ok := opRaw.(map[string]interface{})
				if !ok {
					continue
				}
				tags, _ := op["tags"].([]interface{})
				match := false
				for _, t := range tags {
					if s, _ := t.(string); s == domain {
						match = true
						break
					}
				}
				if !match {
					delete(pathItem, method)
					continue
				}
				op["x-can-try-it"] = canTry
				keptMethods++
			}
			if keptMethods == 0 {
				delete(pathsRaw, path)
			}
		}
	}

	respondJSON(w, http.StatusOK, filtered)
}

// deepCloneJSON 深拷贝一个 unmarshal 出来的 JSON 树
func deepCloneJSON(in interface{}) map[string]interface{} {
	b, _ := json.Marshal(in)
	var out map[string]interface{}
	_ = json.Unmarshal(b, &out)
	return out
}
