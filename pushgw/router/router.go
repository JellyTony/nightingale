package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ccfos/nightingale/v6/memsto"
	"github.com/ccfos/nightingale/v6/pkg/ctx"
	"github.com/ccfos/nightingale/v6/pkg/httpx"
	"github.com/ccfos/nightingale/v6/pushgw/idents"
	"github.com/ccfos/nightingale/v6/pushgw/pconf"
	"github.com/ccfos/nightingale/v6/pushgw/writer"
)

type Router struct {
	HTTP           httpx.Config
	Pushgw         pconf.Pushgw
	TargetCache    *memsto.TargetCacheType
	BusiGroupCache *memsto.BusiGroupCacheType
	IdentSet       *idents.Set
	Writers        *writer.WritersType
	Ctx            *ctx.Context
}

func New(httpConfig httpx.Config, pushgw pconf.Pushgw, tc *memsto.TargetCacheType, bg *memsto.BusiGroupCacheType, set *idents.Set, writers *writer.WritersType, ctx *ctx.Context) *Router {
	return &Router{
		IdentSet:       set,
		HTTP:           httpConfig,
		Writers:        writers,
		Ctx:            ctx,
		TargetCache:    tc,
		BusiGroupCache: bg,
	}
}

func (rt *Router) Config(r *gin.Engine) {
	registerMetrics()

	// datadog url: http://n9e-pushgw.foo.com/datadog
	// use apiKey not basic auth
	r.POST("/datadog/api/v1/series", rt.datadogSeries)
	r.POST("/datadog/api/v1/check_run", datadogCheckRun)
	r.GET("/datadog/api/v1/validate", datadogValidate)
	r.POST("/datadog/api/v1/metadata", datadogMetadata)
	r.POST("/datadog/intake/", datadogIntake)

	if len(rt.HTTP.BasicAuth) > 0 {
		// enable basic auth
		auth := gin.BasicAuth(rt.HTTP.BasicAuth)
		r.POST("/opentsdb/put", auth, rt.openTSDBPut)
		r.POST("/openfalcon/push", auth, rt.falconPush)
		r.POST("/prometheus/v1/write", auth, rt.remoteWrite)
	} else {
		// no need basic auth
		r.POST("/opentsdb/put", rt.openTSDBPut)
		r.POST("/openfalcon/push", rt.falconPush)
		r.POST("/prometheus/v1/write", rt.remoteWrite)
	}
}
