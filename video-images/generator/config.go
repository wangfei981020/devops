package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
)

// Stream 表示一路要抽帧的直播流
type Stream struct {
	Cluster    string // 所属 section,如 cluster_1(仅用于日志)
	Name       string // filename,如 D001;既是分片键,也是对象名(去掉 .jpg)
	InputURL   string // rtmp://host/path?vhost=xxx
	OutputPath string // 输出目录,如 /data/images
	OutputFile string // Name + ".jpg"
}

// ParseConfig 解析 config.ini,完全复刻生产 Python 的语义:
//   - 以 ; 或 # 开头的整行为注释(= 被禁用的流),跳过
//   - [section] 内 input_domain/input_vhost/output_path 为公共项
//   - 其余 NN=/path filename=X 为一路流
func ParseConfig(path string) ([]Stream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var streams []Stream
	var section, domain, vhost, outPath string

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue // 空行 / 注释(= 被禁用的流)
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			domain, vhost, outPath = "", "", ""
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			log.Printf("WARN config 第%d行无法识别(缺=): %q", lineNo, line)
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		switch key {
		case "input_domain":
			domain = val
		case "input_vhost":
			vhost = val
		case "output_path":
			outPath = val
		default:
			// 一路流: val = "/live/6-11 filename=D001"
			parts := strings.SplitN(val, " ", 2)
			if len(parts) != 2 {
				log.Printf("WARN config 第%d行流格式异常(缺空格): %q", lineNo, line)
				continue
			}
			streamPath := strings.TrimSpace(parts[0])
			fnKV := strings.SplitN(strings.TrimSpace(parts[1]), "=", 2)
			if len(fnKV) != 2 || strings.TrimSpace(fnKV[0]) != "filename" {
				log.Printf("WARN config 第%d行 filename 缺失: %q", lineNo, line)
				continue
			}
			name := strings.TrimSpace(fnKV[1])
			if domain == "" || outPath == "" {
				log.Printf("WARN config 第%d行流 %s 缺 input_domain/output_path,跳过", lineNo, name)
				continue
			}
			input := domain + streamPath
			if vhost != "" {
				input += "?vhost=" + vhost
			}
			streams = append(streams, Stream{
				Cluster:    section,
				Name:       name,
				InputURL:   input,
				OutputPath: outPath,
				OutputFile: name + ".jpg",
			})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取 config 失败: %w", err)
	}
	return streams, nil
}
