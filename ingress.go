package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// ingress.go
// 节点入口处理：DNS 裂变、SNI 补全、失败和去重统计。

// ingress 负责节点入口处理，更新 ctx.Nodes 和 ctx.AirportStats
func ingress(ctx *UpdateContext) {
	newNodes := []Node{}
	uniqueSet := make(map[string]struct{})
	dnsCache := make(map[string][]string)

	for _, node := range ctx.Nodes {
		stat := ctx.AirportStats[node.Source]
		if stat == nil {
			stat = &Stat{}
			ctx.AirportStats[node.Source] = stat
		}
		stat.Total++

		// 只查域名，IP节点直接保留
		if isIP(node.Server) {
			// IP 节点直接使用，不需要 DNS 查询
			key := uniqueKey(node)
			if _, exists := uniqueSet[key]; !exists {
				uniqueSet[key] = struct{}{}
				newNodes = append(newNodes, node)
			} else {
				stat.Duplicated++
			}
			continue
		}

		// DNS 缓存
		ips, ok := dnsCache[node.Server]
		if !ok {
			ips, _ = resolveA1_1_1_1(node.Server)
			dnsCache[node.Server] = ips
		}
		if len(ips) == 0 {
			Warn("INGRESS", "DoH 查询失败: [%s] %s", node.Source, node.OriginName)
			stat.Failed++
			continue
		}

		// 裂变：一个域名节点变成多个 IP 节点，使用新的 server（IP）进行去重
		originalServer := node.Server // 保存原始域名用于 SNI 补全
		added := false
		for _, ip := range ips {
			n := node
			n.Server = ip // 更新为 IP 地址
			if needSNI(n.Type) && n.Params["sni"] == "" && isDomain(originalServer) {
				n.Params["sni"] = originalServer // 使用原始域名作为 SNI
			}
			// 使用新的 server（IP）和 port 生成唯一 key
			key := uniqueKey(n)
			if _, exists := uniqueSet[key]; !exists {
				uniqueSet[key] = struct{}{}
				newNodes = append(newNodes, n)
				added = true
			}
		}
		// 如果这个域名节点的所有 IP 都被去重了，则算作被去重
		if !added {
			stat.Duplicated++
		}
	}
	ctx.Nodes = newNodes

	// 输出每个机场的统计日志，格式: [机场名] 总数=%d 去重=%d 失败=%d
	for airport, stat := range ctx.AirportStats {
		Info("INGRESS", "[%s] 总数=%d 去重=%d 失败=%d", airport, stat.Total, stat.Duplicated, stat.Failed)
	}
}

// 判断是否为IP
func isIP(server string) bool {
	return net.ParseIP(server) != nil
}

// 使用 Cloudflare 1.1.1.1 DoH 查询 A 记录
func resolveA1_1_1_1(domain string) ([]string, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest("GET", "https://1.1.1.1/dns-query?name="+domain+"&type=A", nil)
	req.Header.Set("accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result struct {
		Answer []struct {
			Data string `json:"data"`
			Type int    `json:"type"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	var ips []string
	for _, ans := range result.Answer {
		if ans.Type == 1 { // A 记录
			ips = append(ips, ans.Data)
		}
	}
	return ips, nil
}

// needSNI 判断节点类型是否需要 SNI
func needSNI(typ string) bool {
	// 可根据业务扩展
	return typ == "trojan" || typ == "vmess"
}

// isDomain 判断 server 是否为域名
func isDomain(server string) bool {
	return net.ParseIP(server) == nil && strings.Contains(server, ".")
}

// uniqueKey 生成节点去重用的唯一 key
func uniqueKey(n Node) string {
	// 基于原始域名进行去重，而不是 DNS 查询后的 IP
	// 这样不同的域名即使解析到同一个 IP 也不会被认为是重复的
	return fmt.Sprintf("%s|%s|%s", n.Type, n.Server, n.Port)
}
