package limestone

import (
	"reflect"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

var sampleCaddyfile = []byte(`{
	limestone {
		docker_network internal
		default_pool web

		pool web {
			image traefik/whoami
			backend_port 80
			health_path /
			min 1
			max 4
			target_inflight 2
			scale_out_cooldown 3s
			scale_in_cooldown 15s
			drain_grace 5s
		}
	}
}
`)

var sampleApp = &App{
	DockerNetwork: "internal",
	DefaultPool:   "web",
	Pools: []PoolConfig{
		{
			Name:             "web",
			Image:            "traefik/whoami",
			BackendPort:      80,
			HealthPath:       "/",
			Min:              1,
			Max:              4,
			TargetInflight:   2,
			ScaleOutCooldown: 3 * time.Second,
			ScaleInCooldown:  15 * time.Second,
			DrainGrace:       5 * time.Second,
		},
	},
}

func TestUnmarshalCaddyfile(t *testing.T) {
	app := new(App)

	tokens, err := caddyfile.Tokenize(sampleCaddyfile, "caddyfile")
	if err != nil {
		t.Error(err)
	}

	err = app.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
	if err != nil {
		t.Error(err)
	}

	if !reflect.DeepEqual(app, sampleApp) {
		t.Errorf("Incorrect Result: \nresult %v \nexpected: %v", app, sampleApp)
	}
}
