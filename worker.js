export default {
  async fetch(request, env) {
    const { method, headers: reqHeaders, url: reqUrl } = request
    const url = new URL(reqUrl)

    // 1. CORS 预检 (OPTIONS)
    if (method === "OPTIONS") {
      // 如果客户端预检里没带任何头，就允许所有自定义头
      const allowReqHdrs = reqHeaders.get("Access-Control-Request-Headers") || "*"
      const respHeaders = new Headers({
        "Access-Control-Allow-Origin":  "*",
        "Access-Control-Allow-Methods": "GET,OPTIONS",
        "Access-Control-Allow-Headers": allowReqHdrs,
        "Access-Control-Max-Age":       "604800"   // 7 天
      })
      return new Response(null, { status: 204, headers: respHeaders })
    }

    // 2. Token 校验
    const t = url.searchParams.get("t")
    if (!t || t !== env.TOKEN) {
      return new Response("Unauthorized", { status: 401 })
    }

    // 3. 构造上游请求头，透传 Accept-Encoding & Range
    const upstreamHeaders = new Headers()
    for (const name of ["accept-encoding", "range"]) {
      const v = reqHeaders.get(name)
      if (v) upstreamHeaders.set(name, v)
    }
    upstreamHeaders.set("Accept-Language", "en-US,en;q=0.9")

    // 4. 从环境变量读取 Gist 原始 URL
    const gistUrl = env.GIST_URL
    if (!gistUrl) {
      return new Response("GIST_URL not set", { status: 500 })
    }

    // 5. 发起上游请求
    const upstreamResponse = await fetch(gistUrl, {
      method:   method,
      headers:  upstreamHeaders,
      redirect: "follow",
    })

    // 6. 处理参数修改
    let content = await upstreamResponse.text()
    content = processNodes(content, url.searchParams)

    // 7. 克隆上游头部，并注入 Connection: close + CORS
    const respHeaders = new Headers(upstreamResponse.headers)
    respHeaders.set("Connection", "close")               // HTTP/1.1 下立即拆 TCP
    respHeaders.set("Access-Control-Allow-Origin", "*")  // 让浏览器接受跨域响应
    respHeaders.set("Content-Type", "text/plain; charset=utf-8")

    // 8. 返回修改后的内容
    return new Response(content, {
      status:     upstreamResponse.status,
      statusText: upstreamResponse.statusText,
      headers:    respHeaders,
    })
  },
}

// 处理节点参数覆盖
function processNodes(content, params) {
  // 定义允许的参数映射（URL参数名 -> 节点属性名）
  const paramMap = {
    "udp": "udp-relay",
    "quic": "block-quic", 
    "tfo": "tfo"
  }

  const lines = content.split('\n')
  const result = []

  for (const line of lines) {
    if (!line.trim()) {
      result.push(line)
      continue
    }

    let modifiedLine = line

    // 处理参数覆盖（只处理在paramMap中定义的参数）
    for (const [key, value] of params.entries()) {
      const attr = paramMap[key]
      if (attr) {
        modifiedLine = replaceAttr(modifiedLine, attr, value)
      }
    }

    // 处理参数新增（只处理在paramMap中定义的参数）
    for (const [key, value] of params.entries()) {
      const attr = paramMap[key]
      if (attr) {
        const attrEq = attr + "="
        if (!modifiedLine.includes(attrEq)) {
          // 直接在行尾添加逗号和参数
          modifiedLine += "," + attr + "=" + value
        }
      }
    }

    result.push(modifiedLine)
  }

  return result.join('\n')
}

// 替换节点属性值，仅替换等号后第一个逗号或行尾
function replaceAttr(line, attr, val) {
  const prefix = attr + "="
  const idx = line.indexOf(prefix)
  if (idx === -1) {
    return line
  }
  
  const start = idx + prefix.length
  const end = line.indexOf(",", start)
  
  if (end === -1) {
    return line.substring(0, start) + val
  }
  
  return line.substring(0, start) + val + line.substring(end)
} 