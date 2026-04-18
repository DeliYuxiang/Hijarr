package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"hijarr/internal/logger"

	"github.com/gin-gonic/gin"
)

var log = logger.For("proxy")


// forwardHeaders 将原始请求的 header 复制到新请求，跳过 hop-by-hop header。
func forwardHeaders(dst *http.Request, src *http.Request) {
	for k, vv := range src.Header {
		lower := strings.ToLower(k)
		if lower == "host" || lower == "content-length" || lower == "accept-encoding" {
			continue
		}
		for _, v := range vv {
			dst.Header.Add(k, v)
		}
	}
}

// copyResponseHeaders 将上游响应的 header 复制到 gin context，跳过编码/长度相关 header。
func copyResponseHeaders(c *gin.Context, resp *http.Response) {
	for k, vv := range resp.Header {
		lower := strings.ToLower(k)
		if lower == "content-encoding" || lower == "transfer-encoding" ||
			lower == "content-length" || lower == "connection" {
			continue
		}
		for _, v := range vv {
			c.Header(k, v)
		}
	}
}

// proxyReq 将 gin 请求透明转发到 targetURL 并将响应写回 gin context。
func proxyReq(c *gin.Context, targetURL string) {
	var reqBody []byte
	if c.Request.Body != nil {
		reqBody, _ = io.ReadAll(c.Request.Body)
	}
	req, _ := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(reqBody))

	forwardHeaders(req, c.Request)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	copyResponseHeaders(c, resp)
	c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), respBody)
}
