package middleware

import (
	"context"
	"strings"

	"github.com/gobuffalo/buffalo"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	olog "github.com/opentracing/opentracing-go/log"

	"github.com/micro/go-micro/metadata"
)

var tracer opentracing.Tracer

//OpenTracing is a buffalo middleware that adds the necessary
//components to the request to make it traced through OpenTracing
func OpenTracing(tr opentracing.Tracer) buffalo.MiddlewareFunc {
	tracer = tr
	return func(next buffalo.Handler) buffalo.Handler {
		return func(c buffalo.Context) error {
			opName := "HTTP " + c.Request().Method + c.Request().URL.Path

			rt := c.Value("current_route")
			if rt != nil {
				route, ok := rt.(buffalo.RouteInfo)
				if ok {
					opName = operation(route.HandlerName)
				}
			}
			sp := tr.StartSpan(opName)
			ext.HTTPMethod.Set(sp, c.Request().Method)
			ext.HTTPUrl.Set(sp, c.Request().URL.String())

			ext.Component.Set(sp, "buffalo")
			c.Set("otspan", sp)
			err := next(c)
			if err != nil {
				ext.Error.Set(sp, true)
				sp.LogFields(olog.Error(err))
			}
			br, ok := c.Response().(*buffalo.Response)
			if ok {
				ext.HTTPStatusCode.Set(sp, uint16(br.Status))
			}
			sp.Finish()

			return err
		}
	}
}

// SpanFromContext attempts to retrieve a span from the Buffalo context,
// returning it if found.  If none is found a new one is created.
func SpanFromContext(c buffalo.Context) opentracing.Span {
	// fast path - find span in the buffalo context and return it
	sp := c.Value("otspan")
	if sp != nil {
		span, ok := sp.(opentracing.Span)
		if ok {
			c.LogField("span found", true)
			return span
		}
	}

	c.LogField("span found", false)
	// none exists, make a new one (sadface)
	opName := "HTTP " + c.Request().Method + c.Request().URL.Path

	rt := c.Value("current_route")
	if rt != nil {
		route, ok := rt.(buffalo.RouteInfo)
		if ok {
			opName = operation(route.HandlerName)
		}
	}
	span := tracer.StartSpan(opName)
	ext.HTTPMethod.Set(span, c.Request().Method)
	ext.HTTPUrl.Set(span, c.Request().URL.String())

	ext.Component.Set(span, "buffalo")
	return span

}

// ChildSpan returns a child span derived from the buffalo context "c"
func ChildSpan(opname string, c buffalo.Context) opentracing.Span {
	psp := SpanFromContext(c)
	sp := opentracing.StartSpan(
		opname,
		opentracing.ChildOf(psp.Context()))
	return sp
}

func operation(s string) string {
	chunks := strings.Split(s, ".")
	return chunks[len(chunks)-1]
}
func MetadataContext(c buffalo.Context) context.Context {
	sp := SpanFromContext(c)
	md := metadata.Metadata{}
	if err := sp.Tracer().Inject(sp.Context(), opentracing.TextMap, opentracing.TextMapCarrier(md)); err != nil {
		return c
	}
	ctx := metadata.NewContext(c, md)
	return ctx
}
