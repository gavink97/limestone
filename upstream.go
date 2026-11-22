package limestone

import (
	"errors"
	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

type PoolSource struct {
	DefaultPool string `json:"default_pool,omitempty"`
	app         *App
}

func (PoolSource) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.upstreams.limestone_pool",
		New: func() caddy.Module { return new(PoolSource) },
	}
}

func (p *PoolSource) Provision(ctx caddy.Context) error {
	appIface, err := ctx.App("limestone")
	if err != nil {
		return err
	}

	app, ok := appIface.(*App)
	if !ok {
		return errors.New("limestone app is not *App")
	}

	p.app = app
	p.DefaultPool = p.app.DefaultPool

	return nil
}

func (p *PoolSource) GetUpstreams(r *http.Request) ([]*reverseproxy.Upstream, error) {
	pool, _ := r.Context().Value(poolCtxKey{}).(string) // set by the handler
	if pool == "" {
		pool = p.DefaultPool
	}

	var out []*reverseproxy.Upstream
	for _, dial := range p.app.reg.HealthyDials(pool) {
		out = append(out, &reverseproxy.Upstream{Dial: dial})
	}
	return out, nil
}

func init() {
	caddy.RegisterModule(new(PoolSource))
}

var (
	_ caddy.Provisioner           = (*PoolSource)(nil)
	_ reverseproxy.UpstreamSource = (*PoolSource)(nil)
)
