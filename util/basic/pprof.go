package basic

import (
	"fmt"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"io"
	"log"
	"net/http"
	"os"
)

// StartPProfServe 启用pprof分析.
// 地址: http://ip:port/api/v1/pprof/.
func StartPProfServe(port int) {
	gin.DisableConsoleColor()
	gin.SetMode("debug")
	gin.DefaultWriter = io.Discard
	endpoint := fmt.Sprintf(":%d", port)

	r := gin.New()
	r.Use(gin.LoggerWithWriter(os.Stdout))
	r.Use(gin.Recovery())
	pprof.Register(r)
	apiv1 := r.Group("/api/v1")
	pprof.RouteRegister(apiv1, "pprof")

	Srv := &http.Server{
		Addr:    endpoint,
		Handler: r,
	}
	if err := Srv.ListenAndServe(); err != nil {
		log.Fatalf("service serve ERR=%v", err)
	}
}
