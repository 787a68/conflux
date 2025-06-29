package main

import (
	"net/http"
	"os"
	"strings"
)

// server.go
// HTTP 服务，监听 80 端口，处理 /conflux 路由的 API 请求。

// 启动 HTTP 服务
func startServer() {
	http.HandleFunc("/conflux", handleConflux)
	http.ListenAndServe(":80", nil)
}

// 处理 /conflux 路由的主入口
func handleConflux(w http.ResponseWriter, r *http.Request) {
	logRequest(r)
	setCORSHeaders(w, r)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !validateToken(r) {
		Warn("HTTP", "Token 校验失败: %s", r.URL.Query().Get("t"))
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("invalid token"))
		return
	}

	if isForceUpdate(r) {
		Info("HTTP", "收到强制更新请求，异步执行 updateNodes")
		go updateNodes()
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("update triggered"))
		return
	}

	nodeConf := "/data/conflux/node.conf"
	if !nodeConfExists(nodeConf) {
		Warn("HTTP", "node.conf 不存在，异步执行 updateNodes")
		go updateNodes()
		w.WriteHeader(http.StatusNoContent)
		w.Write([]byte("node.conf updating"))
		return
	}

	lines, err := loadNodeConf(nodeConf)
	if err != nil {
		Error("HTTP", "读取 node.conf 失败: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("read node.conf error"))
		return
	}

	params := r.URL.Query()
	result := processNodes(lines, params)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(strings.Join(result, "\n")))
}

// 记录请求日志，包含完整URL和Header
func logRequest(r *http.Request) {
	Info("HTTP", "收到请求: %s %s", r.Method, r.URL.String())
	for k, v := range r.Header {
		Info("HTTP", "Header: %s: %s", k, strings.Join(v, ", "))
	}
}

// 设置 CORS 响应头
func setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
}

// 校验 token 是否有效
func validateToken(r *http.Request) bool {
	token := r.URL.Query().Get("t")
	return token != "" && token == getToken("/data/conflux/token")
}

// 判断是否为强制更新请求
func isForceUpdate(r *http.Request) bool {
	_, ok := r.URL.Query()["f"]
	return ok
}

// 检查 node.conf 是否存在
func nodeConfExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// 加载 node.conf 文件，返回节点行切片
func loadNodeConf(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Split(string(data), "\n"), nil
}

// 处理节点参数覆盖和新增
func processNodes(lines []string, params map[string][]string) []string {
	paramMap := map[string]string{
		"udp":  "udp-relay",
		"quic": "block-quic",
		"tfo":  "tfo",
	}
	var result []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		// 覆盖指定参数
		for k, v := range params {
			attr, ok := paramMap[k]
			if !ok {
				continue
			}
			for _, val := range v {
				line = replaceAttr(line, attr, val)
			}
		}
		// 新增未指定参数（节点中没有该属性时才追加到行尾）
		for k, v := range params {
			if _, ok := paramMap[k]; ok {
				continue
			}
			for _, val := range v {
				attrEq := k + "="
				if !strings.Contains(line, attrEq) {
					if !strings.HasSuffix(line, ",") && !strings.HasSuffix(line, " ") {
						line += ","
					}
					line += k + "=" + val
				}
			}
		}
		result = append(result, line)
	}
	return result
}

// 替换节点属性值，仅替换等号后第一个逗号或行尾
func replaceAttr(line, attr, val string) string {
	prefix := attr + "="
	idx := strings.Index(line, prefix)
	if idx == -1 {
		return line
	}
	start := idx + len(prefix)
	end := strings.Index(line[start:], ",")
	if end == -1 {
		return line[:start] + val
	}
	return line[:start] + val + line[start+end:]
}
