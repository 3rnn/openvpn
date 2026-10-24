package main

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
)

// Viper and the legacy runtime configuration fields are not concurrency safe.
// Runtime configuration changes are confined to these routes. Disk edits require
// a restart: Viper's file watcher mutates shared maps before its callback, making
// it impossible for a callback-local lock to protect authenticated readers.
var configRequestMu sync.RWMutex

func configRequestGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		writing := c.Request.Method == http.MethodPost && (path == "/settings" || path == "/login" || path == "/client/modifyPass")
		if writing {
			configRequestMu.Lock()
			defer configRequestMu.Unlock()
		} else {
			configRequestMu.RLock()
			defer configRequestMu.RUnlock()
		}
		c.Next()
	}
}
