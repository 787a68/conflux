package main

// update.go
// 节点聚合与更新，负责调度更新流程和生成 node.conf。

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Node 结构体：描述单个节点的所有属性
// OriginName: 原始节点名
// Type: 节点类型（如 ss, vmess 等）
// Server: 服务器地址
// Port: 端口
// Params: 节点次要参数（如 encrypt-method, password, tfo, udp-relay 等）
// Source: 机场名
// ISO/Emoji: 出口 geo/emoji
// Failed: 是否在 ingress/egress 任一阶段失败

type Node struct {
	OriginName  string            // 原始节点名
	Type        string            // 节点类型
	Server      string            // 服务器地址
	Port        string            // 端口
	Params      map[string]string // 节点次要参数
	ParamString string            // 原始参数字符串，保持顺序
	Source      string            // 机场名
	ISO         string            // geo
	Emoji       string            // emoji
}

// Stat 结构体：机场统计信息
// Total: 总节点数
// Duplicated: 去重节点数
// Failed: ingress 或 egress 任一阶段失败的节点数

type Stat struct {
	Total      int
	Duplicated int
	Failed     int
}

// UpdateContext 结构体：一次 update 流程的上下文
// Nodes: 所有节点
// AirportStats: 每个机场的统计信息

type UpdateContext struct {
	Nodes        []Node
	AirportStats map[string]*Stat
}

// updateNodes 是节点聚合与更新的主流程，串联各阶段
func updateNodes() {
	// 1. 解析 SUB 环境变量，获取机场名和订阅链接
	subEnv := os.Getenv("SUB")
	airports := parseSubEnv(subEnv)

	// 2. 并发拉取所有机场订阅内容
	rawProxies := fetchAllProxies(airports)

	// 3. 解析节点，过滤无效行，生成 Node 列表
	nodes := parseAllNodes(rawProxies)

	// 4. 创建上下文，初始化机场统计
	ctx := &UpdateContext{
		Nodes:        nodes,
		AirportStats: make(map[string]*Stat),
	}

	// 5. ingress 入口处理（DNS 裂变、SNI 补全、失败统计）
	ingress(ctx)

	// 6. egress 出口检测（geo 检测、失败统计）
	egress(ctx)

	// 7. 节点重命名，生成最终节点名
	renameNodes(ctx)

	// 8. 写入 node.conf
	writeNodeConf(ctx.Nodes)

	// 9. 输出机场统计日志
	logAirportStats(ctx.AirportStats)
}

// 解析 SUB 环境变量，返回 map[机场名]订阅链接
func parseSubEnv(sub string) map[string]string {
	result := make(map[string]string)
	for _, part := range strings.Split(sub, "||") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			result[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
	}
	return result
}

// 并发拉取所有机场订阅内容，返回 map[机场名][]原始行
func fetchAllProxies(airports map[string]string) map[string][]string {
	result := make(map[string][]string)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for name, url := range airports {
		wg.Add(1)
		go func(name, url string) {
			defer wg.Done()
			lines := fetchProxies(name, url)
			mu.Lock()
			result[name] = lines
			mu.Unlock()
		}(name, url)
	}
	wg.Wait()
	return result
}

// 拉取单个机场订阅，返回所有行（失败重试一次，UA 伪装为 Surge）
func fetchProxies(airport, url string) []string {
	client := &http.Client{Timeout: 3 * time.Second}
	for i := 0; i < 2; i++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			Error("UPDATE", "机场 %s 创建请求失败: %v", airport, err)
			continue
		}
		req.Header.Set("User-Agent", "Surge")
		resp, err := client.Do(req)
		if err != nil {
			if i == 1 { // 最后一次重试失败
				Error("UPDATE", "机场 %s 请求失败: %v", airport, err)
			}
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if resp.StatusCode != 200 {
			if i == 1 { // 最后一次重试失败
				Error("UPDATE", "机场 %s HTTP状态码错误: %d", airport, resp.StatusCode)
			}
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if len(lines) == 0 {
			Warn("UPDATE", "机场 %s 返回空内容", airport)
		} else {
			Info("UPDATE", "机场 %s 获取 %d 行内容", airport, len(lines))
		}
		return lines
	}
	Error("UPDATE", "机场 %s 所有重试均失败", airport)
	return nil
}

// 解析所有机场的节点，过滤无效行，返回 Node 列表
func parseAllNodes(rawProxies map[string][]string) []Node {
	nodes := []Node{}
	for airport, lines := range rawProxies {
		for _, line := range extractProxyLines(lines) {
			node, ok := parseNodeLine(line, airport)
			if ok {
				nodes = append(nodes, node)
			}
		}
	}
	return nodes
}

// 提取 [Proxy] 块的节点行，过滤注释、reject、direct
func extractProxyLines(lines []string) []string {
	var result []string
	inProxy := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[Proxy]") {
			inProxy = true
			continue
		}
		if inProxy {
			if line == "" || strings.HasPrefix(line, "[") {
				break
			}
			if !strings.HasPrefix(line, "#") && !strings.Contains(line, "reject") && !strings.Contains(line, "direct") {
				result = append(result, line)
			}
		}
	}
	return result
}

// 解析单行节点，返回 Node 结构体
func parseNodeLine(line, airport string) (Node, bool) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return Node{}, false
	}
	name := strings.TrimSpace(parts[0])
	mainParts := strings.Split(parts[1], ",")
	if len(mainParts) < 3 {
		return Node{}, false
	}
	typeStr := strings.TrimSpace(mainParts[0])
	server := strings.TrimSpace(mainParts[1])
	port := strings.TrimSpace(mainParts[2])
	params := make(map[string]string)

	// 保存参数字符串部分，保持原始顺序
	paramStrings := []string{}
	for _, p := range mainParts[3:] {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
			paramStrings = append(paramStrings, strings.TrimSpace(p))
		}
	}

	return Node{
		OriginName:  name,
		Type:        typeStr,
		Server:      server,
		Port:        port,
		Params:      params,
		ParamString: strings.Join(paramStrings, ","),
		Source:      airport,
	}, true
}

// 节点重命名，生成最终节点名（不覆盖原始名，直接用于输出）
func renameNodes(ctx *UpdateContext) {
	// 按机场和 ISO 分组计数
	counters := make(map[string]map[string]int)

	// 先统计每个机场和 ISO 组合的数量
	for _, node := range ctx.Nodes {
		if counters[node.Source] == nil {
			counters[node.Source] = make(map[string]int)
		}
		counters[node.Source][node.ISO]++
	}

	// 重置计数器用于重命名
	renameCounters := make(map[string]map[string]int)

	// 重命名节点
	for i, node := range ctx.Nodes {
		if renameCounters[node.Source] == nil {
			renameCounters[node.Source] = make(map[string]int)
		}
		renameCounters[node.Source][node.ISO]++
		newName := fmt.Sprintf("%s %s%s-%02d", node.Source, node.ISO, node.Emoji, renameCounters[node.Source][node.ISO])
		ctx.Nodes[i].Params["_newname"] = newName // 仅用于输出，不覆盖 OriginName
	}
}

// 格式化节点为订阅输出格式
// newName: 新节点名（如 AR HK🇭🇰-01）
func formatNode(n Node, newName string) string {
	// 使用原始参数字符串保持顺序
	params := n.ParamString

	// 处理新增的参数（如 ingress 中添加的 sni）
	// 检查是否有新增的参数不在原始字符串中
	originalParams := make(map[string]bool)
	if params != "" {
		for _, p := range strings.Split(params, ",") {
			kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
			if len(kv) == 2 {
				originalParams[kv[0]] = true
			}
		}
	}

	// 添加新增的参数到末尾
	for k, v := range n.Params {
		if k == "_newname" {
			continue // 不输出内部字段
		}
		if !originalParams[k] {
			if params != "" {
				params += ","
			}
			params += k + "=" + v
		}
	}

	return fmt.Sprintf("%s = %s,%s,%s, %s", newName, n.Type, n.Server, n.Port, params)
}

// 写入 node.conf 文件
func writeNodeConf(nodes []Node) {
	// 按机场名和节点名排序
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Source != nodes[j].Source {
			return nodes[i].Source < nodes[j].Source
		}
		return nodes[i].OriginName < nodes[j].OriginName
	})

	lines := []string{}
	for _, node := range nodes {
		newName := node.Params["_newname"]
		if newName == "" {
			newName = node.OriginName
		}
		line := formatNode(node, newName)
		// 使用正则替换 true/false 为 1/0
		line = strings.ReplaceAll(line, "=true", "=1")
		line = strings.ReplaceAll(line, "=false", "=0")
		lines = append(lines, line)
	}
	// 检查内容非空再写入
	content := strings.Join(lines, "\n")
	if strings.TrimSpace(content) != "" {
		_ = os.WriteFile("/data/conflux/node.conf", []byte(content), 0644)
	}
}

// 输出机场统计日志
func logAirportStats(stats map[string]*Stat) {
	totalNodes := 0
	for _, stat := range stats {
		totalNodes += stat.Total
	}
	Info("UPDATE", "总可用节点数: %d", totalNodes)
}
