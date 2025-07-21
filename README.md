# conflux 应用简介

**conflux** 是一个专为 Surge 用户设计的多机场订阅聚合、节点健康检测与智能导出工具。它自动拉取多个机场订阅，进行节点去重、DNS 裂变、SNI 补全、出口 GEO 检测，并最终生成 Surge 格式的节点配置，便于 Surge 客户端直接导入和使用。

---

## 主要特性

- 多机场订阅聚合，自动合并节点
- 节点健康检测，自动 GEO/Emoji 重命名
- 智能去重与 DNS 裂变，SNI 补全
- 输出 Surge 格式节点配置
- 内置 HTTP API，支持 token 认证、参数覆盖、强制刷新
- Docker 极简部署，直接拉取镜像即可运行

---

## 环境变量说明

| 变量名   | 是否必需 | 说明                                                                                      | 示例                                                         |
|----------|---------|-------------------------------------------------------------------------------------------|--------------------------------------------------------------|
| SUB      | 必需    | 机场订阅列表，格式为 `机场名=订阅链接||机场名2=订阅链接2`，支持多个机场聚合                | `机场A=https://xxx/subscribeA||机场B=https://xxx/subscribeB` |
| TOKEN    | 可选    | 访问 API 的认证 token。未设置时自动生成并保存在 `/data/conflux/token`                      | `TOKEN=your_token`                                           |
| GISTS    | 可选    | 自动上传 node.conf 到 GitHub Gists，格式为 `token@gist_id`                                 | `GISTS=ghp_xxx@1234567890abcdef1234567890abcdef`             |

> **说明：**
> - `SUB` 是最核心的环境变量，决定 conflux 拉取哪些机场的节点。
> - `TOKEN` 用于 API 认证，建议设置，防止未授权访问。
> - `GISTS` 仅在需要将节点配置同步到 GitHub Gists 时设置。

---

## URL 参数说明（API 访问时可用）

| 参数名   | 说明                                                                                 | 示例                        |
|----------|--------------------------------------------------------------------------------------|-----------------------------|
| t        | 认证 token，需与环境变量 TOKEN 保持一致或读取自动生成的 token 文件                    | `t=your_token`              |
| f        | 强制刷新节点订阅和检测，触发 updateNodes 流程（**只需带 f，无需赋值**）                | `&f`                        |
| udp      | 覆盖所有节点的 `udp-relay` 参数（1=开启，0=关闭）                                     | `udp=1`                     |
| quic     | 覆盖所有节点的 `block-quic` 参数（1=开启，0=关闭）                                    | `quic=1`                    |
| tfo      | 覆盖所有节点的 `tfo` 参数（1=开启，0=关闭）                                           | `tfo=1`                     |

> **说明：**
> - 只有 `udp`、`quic`、`tfo` 这三个参数支持通过 URL 动态覆盖节点参数，其他参数不支持。
> - 访问 API 时，必须带上 `t` 参数（token），否则无法获取节点配置。
> - **强制刷新（`f`）只需带参数即可，无需赋值。**

---

## Docker 快速使用

直接拉取并运行你的镜像（无需本地构建）：

```bash
docker run -d --name conflux -p 80:80 \
  -e SUB="机场A=https://xxx/subscribeA||机场B=https://xxx/subscribeB" \
  -e TOKEN="your_token" \
  787a68:conflux
```

如需持久化数据（如日志、配置），可挂载数据目录：

```bash
docker run -d --name conflux -p 80:80 \
  -v /your/data/dir:/data/conflux \
  -e SUB="..." \
  787a68:conflux
```

---

## Surge 订阅配置

在 Surge 的 [Proxy] 订阅栏填写：

```
http://<your_host>:80/conflux?t=your_token
```

如需强制刷新节点：
```
http://<your_host>:80/conflux?t=your_token&f
```

如需动态控制节点 UDP、QUIC、TFO：
```
http://<your_host>:80/conflux?t=your_token&udp=1&quic=0&tfo=1
```

---

## 适用范围

- **仅适用于 Surge**，输出节点格式与 Surge 完全兼容。
- 适合多机场用户自动聚合、健康检测、智能导出节点。
- 通过 API 可灵活控制节点参数，满足不同网络环境需求。

--- 

---

## 配套 Worker 反代（可选）

**worker.js 的作用**：

- 作为 Cloudflare Worker（或其他平台 Serverless Worker）部署，
- 反向代理 Gist 上的节点配置文件（node.conf），
- 支持通过自定义域名/CDN 加速访问节点，
- 支持 URL 参数动态覆盖节点属性（如 udp、quic、tfo），
- 支持 token 权限校验，提升安全性，
- 便于 Surge 用户在国内外网络环境下更快、更稳定地获取节点配置。

**worker.js 已支持通过环境变量 GIST_URL 配置 Gist 地址，无需修改代码即可切换目标 Gist。**

### 使用方法

1. 将仓库中的 `worker.js` 部署到 Cloudflare Worker/Vercel 等平台。
2. 设置环境变量 `GIST_URL` 为你的 Gist 原始链接（如 `https://gist.githubusercontent.com/xxx/raw/node.conf`）。
3. 设置环境变量 `TOKEN`，与主服务保持一致，实现安全访问。
4. 访问你的 Worker 域名即可获取最新节点配置。

### Cloudflare Worker 代码片段（已集成在 worker.js 中）

```js
// 只需配置 GIST_URL 和 TOKEN 环境变量即可
const gistUrl = env.GIST_URL;
const token = env.TOKEN;
```

> Cloudflare 部署时，在 Worker 设置中添加环境变量 `GIST_URL` 和 `TOKEN`。 