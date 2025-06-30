package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
)

// egress.go
// 节点出口检测，获取出口 ISO 代码和 emoji。

// egress 负责 geo 检测、出口检测、失败统计
func egress(ctx *UpdateContext) {
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 10) // 限制并发数

	for i := range ctx.Nodes {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}        // 获取信号量
			defer func() { <-semaphore }() // 释放信号量

			node := &ctx.Nodes[index]
			detectNodeGeo(node, ctx)
		}(i)
	}

	wg.Wait()

	// 过滤掉检测失败的节点
	successfulNodes := []Node{}
	for _, node := range ctx.Nodes {
		if node.ISO != "" && node.Emoji != "" {
			successfulNodes = append(successfulNodes, node)
		}
	}
	ctx.Nodes = successfulNodes

	// 重新计算每个机场的统计信息
	for airport, stat := range ctx.AirportStats {
		// 重新计算总数为成功检测的数量
		successCount := 0
		for _, node := range ctx.Nodes {
			if node.Source == airport {
				successCount++
			}
		}
		stat.Total = successCount
	}

	// 输出每个机场的统计日志
	for airport, stat := range ctx.AirportStats {
		Info("EGRESS", "[%s] 总数=%d 去重=%d 失败=%d", airport, stat.Total, stat.Duplicated, stat.Failed)
	}
}

// detectNodeGeo 检测单个节点的地理位置
func detectNodeGeo(node *Node, ctx *UpdateContext) {
	// 转换 Surge 参数格式
	proxyMap := convertNodeToProxyMap(node)

	// 创建代理客户端
	client := createProxyClient(proxyMap)
	if client == nil {
		Warn("EGRESS", "创建代理客户端失败: [%s] %s", node.Source, node.OriginName)
		updateFailedCount(node.Source, ctx)
		return
	}

	// 通过代理访问 Cloudflare trace 接口获取 ISO
	iso, err := getProxyISO(client)
	if err != nil {
		Warn("EGRESS", "获取 ISO 失败: [%s] %s - %v", node.Source, node.OriginName, err)
		updateFailedCount(node.Source, ctx)
		return
	}

	// 根据 ISO 计算 emoji
	emoji := getEmojiByISO(iso)

	// 更新节点信息
	node.ISO = iso
	node.Emoji = emoji
}

// convertNodeToProxyMap 将 Node 转换为代理映射，处理参数转换
func convertNodeToProxyMap(node *Node) map[string]interface{} {
	proxyMap := map[string]interface{}{
		"name":   node.OriginName,
		"type":   node.Type,
		"server": node.Server,
		"port":   node.Port,
	}

	if node.Type == "vmess" {
		alterId := 1 // 默认旧协议
		if val, ok := node.Params["vmess-aead"]; ok && (val == "true" || val == "1") {
			alterId = 0 // AEAD
		}
		proxyMap["alterId"] = alterId
	}

	for k, v := range node.Params {
		if node.Type == "vmess" && k == "vmess-aead" {
			continue // 不输出 vmess-aead
		}
		newKey := convertParamName(k)
		newValue := convertParamValue(v)
		proxyMap[newKey] = newValue
	}

	return proxyMap
}

// convertParamName 转换参数名
func convertParamName(key string) string {
	switch key {
	case "encrypt-method":
		return "cipher"
	case "udp-relay":
		return "udp"
	case "username":
		return "uuid"
	default:
		return key
	}
}

// convertParamValue 转换参数值（字符串转数值或布尔值）
func convertParamValue(value string) interface{} {
	// 尝试转换为布尔值
	if value == "true" || value == "1" {
		return true
	}
	if value == "false" || value == "0" {
		return false
	}

	// 尝试转换为数字
	if num, err := strconv.Atoi(value); err == nil {
		return num
	}
	// 尝试转换为浮点数
	if num, err := strconv.ParseFloat(value, 64); err == nil {
		return num
	}
	// 如果不是数字，保持原字符串值
	return value
}

// createProxyClient 创建代理客户端
func createProxyClient(proxyMap map[string]interface{}) *http.Client {
	// 使用 mihomo 库创建代理
	proxy, err := adapter.ParseProxy(proxyMap)
	if err != nil {
		return nil
	}

	// 创建自定义 Transport
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			var u16Port uint16
			if portNum, err := strconv.ParseUint(port, 10, 16); err == nil {
				u16Port = uint16(portNum)
			}
			return proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: u16Port,
			})
		},
		IdleConnTimeout:   3 * time.Second,
		DisableKeepAlives: true,
	}

	return &http.Client{
		Timeout:   3 * time.Second,
		Transport: transport,
	}
}

// getProxyISO 通过代理获取 ISO 国家代码
func getProxyISO(client *http.Client) (string, error) {
	// 轮询 1.1.1.1 和 1.0.0.1
	urls := []string{
		"https://1.1.1.1/cdn-cgi/trace",
		"https://1.0.0.1/cdn-cgi/trace",
	}

	for _, url := range urls {
		// 访问 Cloudflare trace 接口
		resp, err := client.Get(url)
		if err != nil {
			continue // 尝试下一个地址
		}
		defer resp.Body.Close()

		// 读取响应内容
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue // 尝试下一个地址
		}

		// 解析响应获取 ISO
		// 响应格式类似：loc=HK
		content := string(body)
		lines := strings.Split(content, "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "loc=") {
				iso := strings.TrimPrefix(line, "loc=")
				return iso, nil
			}
		}
	}

	return "", fmt.Errorf("无法获取 ISO 代码")
}

// getEmojiByISO 根据 ISO 代码计算 emoji
func getEmojiByISO(iso string) string {
	// 其他 ISO 代码转换为 emoji
	// 使用 Unicode 区域指示符符号
	// 将 ISO 代码转换为对应的 emoji
	// 例如：US -> 🇺🇸, HK -> 🇭🇰, JP -> 🇯🇵

	// 这里需要实现完整的 ISO 到 emoji 的映射
	// 可以使用 Unicode 区域指示符符号 (U+1F1E6 到 U+1F1FF)
	// 每个国家代码对应两个字母，转换为对应的 Unicode 字符

	// 简单的映射示例
	emojiMap := map[string]string{
		"US": "🇺🇸", "HK": "🇭🇰", "JP": "🇯🇵", "SG": "🇸🇬",
		"KR": "🇰🇷", "TW": "🌏", "GB": "🇬🇧", "DE": "🇩🇪",
		"FR": "🇫🇷", "CA": "🇨🇦", "AU": "🇦🇺", "NL": "🇳🇱",
	}

	if emoji, exists := emojiMap[iso]; exists {
		return emoji
	}

	// 如果没有预定义映射，使用 Unicode 计算
	return calculateEmojiFromISO(iso)
}

// calculateEmojiFromISO 根据 ISO 代码计算 emoji
func calculateEmojiFromISO(iso string) string {

	// Unicode 区域指示符符号范围：U+1F1E6 (A) 到 U+1F1FF (Z)
	// 将 ISO 代码的两个字母转换为对应的 Unicode 字符
	first := rune(iso[0]) - 'A' + 0x1F1E6
	second := rune(iso[1]) - 'A' + 0x1F1E6

	return string([]rune{first, second})
}

// updateFailedCount 更新失败计数
func updateFailedCount(airport string, ctx *UpdateContext) {
	if stat, exists := ctx.AirportStats[airport]; exists {
		stat.Failed++
	}
}
