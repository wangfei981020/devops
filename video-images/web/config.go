package main

import (
	"bufio"
	"os"
	"strings"
)

// ParseConfigNames 解析 config.ini,返回全部配置流的 "filename.jpg" 集合(跳过 ; 禁用的)。
// 让画廊能显示"配了但从没出图"的失败流。格式与生成端一致。
func ParseConfigNames(path string) map[string]bool {
	names := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()

	specials := map[string]bool{"input_domain": true, "input_vhost": true, "output_path": true}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		if specials[key] {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		parts := strings.SplitN(val, " ", 2)
		if len(parts) != 2 {
			continue
		}
		fnKV := strings.SplitN(strings.TrimSpace(parts[1]), "=", 2)
		if len(fnKV) != 2 || strings.TrimSpace(fnKV[0]) != "filename" {
			continue
		}
		if name := strings.TrimSpace(fnKV[1]); name != "" {
			names[name+".jpg"] = true
		}
	}
	return names
}
