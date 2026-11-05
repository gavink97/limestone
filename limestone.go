package limestone

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/moby/moby/client"
	"go.uber.org/zap"
)

// ownerID survives config reloads but changes on restart
var ownerID = randHex(8)

type PoolConfig struct {
	Name             string        `json:"name"`
	Image            string        `json:"image"`
	BackendPort      int           `json:"backend_port,omitempty"`
	HealthPath       string        `json:"health_path,omitempty"`
	Min              int           `json:"min,omitempty"`
	Max              int           `json:"max,omitempty"`
	TargetInflight   int           `json:"target_inflight,omitempty"`
	ScaleOutCooldown time.Duration `json:"scale_out_cooldown,omitempty"`
	ScaleInCooldown  time.Duration `json:"scale_in_cooldown,omitempty"`
	DrainGrace       time.Duration `json:"drain_grace,omitempty"`
}

func defaultPoolConfig() PoolConfig {
	return PoolConfig{
		Name:             "",
		Image:            "",
		BackendPort:      0,
		HealthPath:       "/",
		Min:              1,
		Max:              10,
		TargetInflight:   20,
		ScaleOutCooldown: 5 * time.Second,
		ScaleInCooldown:  30 * time.Second,
		DrainGrace:       10 * time.Second,
	}
}

type poolState struct {
	cfg       PoolConfig
	lastScale time.Time
	busy      atomic.Bool
}

type routeRule struct {
	prefix string
	pool   string
}

type App struct {
	DefaultPool   string         `json:"default_pool,omitempty"`
	DockerNetwork string         `json:"docker_network,omitempty"`
	TickInterval  caddy.Duration `json:"tick_interval,omitempty"`
	SpawnTimeout  caddy.Duration `json:"spawn_timeout,omitempty"`
	KillOnStop    bool           `json:"kill_on_stop,omitempty"`
	Pools         []PoolConfig   `json:"pools,omitempty"`

	rules       []routeRule
	reg         *Registry
	demand      chan string
	prov        *DockerProvisioner
	probeClient *http.Client
	states      map[string]*poolState
	logger      *zap.Logger
	cancel      context.CancelFunc
}

func (App) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "limestone",
		New: func() caddy.Module { return new(App) },
	}
}

func (a *App) Provision(ctx caddy.Context) error {
	a.logger = ctx.Logger()
	a.demand = make(chan string, 1)
	a.reg = NewRegistry(a.demand)

	if a.TickInterval == 0 {
		a.TickInterval = caddy.Duration(time.Second)
	}

	if a.SpawnTimeout == 0 {
		a.SpawnTimeout = caddy.Duration(45 * time.Second)
	}

	if a.DockerNetwork == "" {
		a.DockerNetwork = "bridge"
	}

	if a.DefaultPool == "" {
		return fmt.Errorf("default_pool is required")
	}

	a.states = make(map[string]*poolState, len(a.Pools))
	defaultPool := defaultPoolConfig()

	for i := range a.Pools {
		pc := a.Pools[i]

		if pc.HealthPath == "" {
			pc.HealthPath = defaultPool.HealthPath
		}

		if pc.Max == 0 {
			pc.Max = defaultPool.Max
		}

		if pc.TargetInflight == 0 {
			pc.TargetInflight = defaultPool.TargetInflight
		}

		if pc.ScaleOutCooldown == 0 {
			pc.ScaleOutCooldown = defaultPool.ScaleOutCooldown
		}

		if pc.ScaleInCooldown == 0 {
			pc.ScaleInCooldown = defaultPool.ScaleInCooldown
		}

		if pc.DrainGrace == 0 {
			pc.DrainGrace = defaultPool.DrainGrace
		}

		a.Pools[i] = pc
		a.states[pc.Name] = &poolState{cfg: pc}
	}

	if _, ok := a.states[a.DefaultPool]; !ok {
		return fmt.Errorf("default_pool %q is not a defined pool", a.DefaultPool)
	}

	for _, r := range a.rules {
		if _, ok := a.states[r.pool]; !ok {
			return fmt.Errorf("route %q references undefined pool %q", r.prefix, r.pool)
		}
	}

	cli, err := client.New(client.FromEnv, client.WithAPIVersionFromEnv())
	if err != nil {
		return err
	}

	a.probeClient = &http.Client{Timeout: 2 * time.Second}
	a.prov = &DockerProvisioner{
		cli:          cli,
		network:      a.DockerNetwork,
		owner:        ownerID,
		spawnTimeout: time.Duration(a.SpawnTimeout),
		probe:        a.probeClient,
	}

	return nil
}

func (a *App) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	orphans, err := a.prov.Adopt(ctx, a.states)
	if err != nil {
		a.logger.Error("adopt failed", zap.Error(err))
	}
	for _, o := range orphans {
		a.reg.AddMember(o.Pool, o.ID, o.Dial, true)
		a.logger.Info("adopted instance",
			zap.String("pool", o.Pool), zap.String("id", o.ID), zap.String("dial", o.Dial))
	}

	go a.controlLoop(ctx)
	return nil
}

func (a *App) Stop() error {
	if a.cancel != nil {
		a.cancel()
	}

	if !a.KillOnStop || a.prov == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, st := range a.states {
		for _, m := range a.reg.Members(st.cfg.Name) {
			wg.Add(1)
			wg.Go(
				func() {
					defer wg.Done()
					err := a.prov.Stop(ctx, m.ID)

					if err != nil {
						a.logger.Error(
							"failed to kill instance",
							zap.String("id", m.ID), zap.Error(err))
					}
				})
		}
	}

	wg.Wait()
	return nil
}

func (a *App) Cleanup() error {
	if a.prov != nil && a.prov.cli != nil {
		return a.prov.cli.Close()
	}
	return nil
}

// Route decides which pool serves a request. STAND-IN for the DB-backed
// resolver: replace with a TTL-cached lookup + singleflight when you wire
// in a real database.
func (a *App) Route(r *http.Request) string {
	for _, rule := range a.rules {
		if strings.HasPrefix(r.URL.Path, rule.prefix) {
			return rule.pool
		}
	}
	return a.DefaultPool
}

func init() {
	caddy.RegisterModule(new(App))
	httpcaddyfile.RegisterGlobalOption("limestone", parseOptLimestone)
}

func parseOptLimestone(d *caddyfile.Dispenser, existing any) (any, error) {
	app := new(App)
	if err := app.UnmarshalCaddyfile(d); err != nil {
		return nil, err
	}

	val := caddyconfig.JSON(app, nil)
	return httpcaddyfile.App{Name: "limestone", Value: val}, nil
}

var (
	_ caddy.App             = (*App)(nil)
	_ caddy.Provisioner     = (*App)(nil)
	_ caddy.CleanerUpper    = (*App)(nil)
	_ caddyfile.Unmarshaler = (*App)(nil)
)
