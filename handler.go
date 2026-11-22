package limestone

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

type poolCtxKey struct{}

type Handler struct {
	app          *App
	WaitCapacity time.Duration `json:"wait_capacity,omitempty"`
}

func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.limestone",
		New: func() caddy.Module { return new(Handler) },
	}
}

func (h *Handler) Provision(ctx caddy.Context) error {
	appIface, err := ctx.App("limestone")
	if err != nil {
		return fmt.Errorf("limestone handler requires the limestone app: %v", err)
	}

	app, ok := appIface.(*App)
	if !ok {
		return errors.New("limestone app is not *App")
	}

	h.app = app
	if h.WaitCapacity == 0 {
		h.WaitCapacity = defaultPoolConfig().ScaleOutCooldown
	}

	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	pool := h.app.Route(r)

	g := h.app.reg.Inflight(pool)
	g.Add(1)
	defer g.Add(-1)

	h.app.reg.Nudge(pool)

	if h.app.reg.HealthyCount(pool) == 0 {
		if err := h.app.reg.WaitHealthy(r.Context(), pool, h.WaitCapacity); err != nil {
			return caddyhttp.Error(http.StatusServiceUnavailable,
				fmt.Errorf("pool %q has no capacity: %v", pool, err))
		}
	}

	// Scatter-gather / pipeline / race against members of this pool, etc

	return next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), poolCtxKey{}, pool)))
}

func init() {
	caddy.RegisterModule(new(Handler))
	httpcaddyfile.RegisterHandlerDirective("limestone", func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		return &Handler{}, nil
	})
}

var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
