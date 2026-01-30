package api

import (
	"net"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"lightobs/internal/server/storage"
	"lightobs/pkg/model"
)

type Handlers struct {
	store storage.Store
}

func NewHandlers(store storage.Store) *Handlers {
	return &Handlers{store: store}
}

func (h *Handlers) Upload(c *gin.Context) {
	var logEntry model.TrafficLog
	if err := c.ShouldBindJSON(&logEntry); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON 解析失败：" + err.Error()})
		return
	}

	// 这里做最基本的数据校验，避免脏数据写入数据库。
	if net.ParseIP(logEntry.SrcIP) == nil || net.ParseIP(logEntry.DstIP) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "src_ip/dst_ip 非法"})
		return
	}
	if !validPort(logEntry.SrcPort) || !validPort(logEntry.DstPort) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "src_port/dst_port 非法"})
		return
	}
	if logEntry.HTTPMethod == "" || logEntry.HTTPPath == "" || logEntry.StatusCode == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "http_method/http_path/status_code 不能为空"})
		return
	}

	if err := h.store.Insert(c.Request.Context(), &logEntry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "写入数据库失败：" + err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handlers) Query(c *gin.Context) {
	ip := c.Query("ip")
	if net.ParseIP(ip) == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ip 参数非法"})
		return
	}

	limit := 200
	if raw := c.Query("limit"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 && v <= 2000 {
			limit = v
		}
	}

	rows, err := h.store.QueryByIP(c.Request.Context(), ip, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询失败：" + err.Error()})
		return
	}

	c.JSON(http.StatusOK, rows)
}

func validPort(p int) bool {
	return p > 0 && p <= 65535
}

