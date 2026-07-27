package ws

import (
	"github.com/gin-gonic/gin"
)

// Mount registers GET /ws/v1/connect onto r.
func Mount(r *gin.Engine, cfg Config) {
	r.GET("/ws/v1/connect", HandleConnect(cfg))
}