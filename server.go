package main

import (
	"fmt"
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

	// 收集所有header信息
	var headers []string
	for k, v := range r.Header {
		headers = append(headers, fmt.Sprintf("%s: %s", k, strings.Join(v, ", ")))
	}

	// 将所有header合并为一条日志
	if len(headers) > 0 {
		Info("HTTP", "Headers: %s", strings.Join(headers, " | "))
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
	// 定义允许的参数映射（URL参数名 -> 节点属性名）
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

		// 处理参数覆盖（只处理在paramMap中定义的参数）
		for k, v := range params {
			attr, ok := paramMap[k]
			if !ok {
				continue // 跳过未定义的参数
			}
			for _, val := range v {
				line = replaceAttr(line, attr, val)
			}
		}

		// 处理参数新增（只处理在paramMap中定义的参数）
		for k, v := range params {
			attr, ok := paramMap[k]
			if !ok {
				continue // 跳过未定义的参数
			}
			for _, val := range v {
				attrEq := attr + "="
				if !strings.Contains(line, attrEq) {
					// 直接在行尾添加逗号和参数
					line += "," + attr + "=" + val
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
