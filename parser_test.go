package limestone

import (
	"reflect"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

var sampleCaddyfiles = [][]byte{
	[]byte(`{
	limestone {
		docker_network internal
		default_pool web

		default_service_provider {
			name vultr
			region ewr
			plan vc2-1c-2gb
		}

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

			service_provider {
				name vultr
				region sgp
				plan vc2-2c-4gb
			}
		}
	}
}
`),
	[]byte(`{
	limestone {
		docker_network internal
		default_pool inventory

		default_service_provider {
			name vultr
			region ewr
			plan vc2-1c-2gb
		}

		default_pool_config {
			min 1
			target_inflight 2
			scale_out_cooldown 3s
			scale_in_cooldown 15s
			drain_grace 5s
		}

		rules {
			/api/catalog catalog
			/api/inventory inventory
			/api/checkout checkout
		}

		pool catalog {
			image traefik/whoami
			backend_port 80
			health_path /
			max 2
		}

		pool inventory {
			image traefik/whoami
			backend_port 80
			health_path /
			max 4
		}

		pool checkout {
			image traefik/whoami
			backend_port 80
			health_path /
			min 2
			max 12
		}


	}
}
`)}

var sampleApps = []*App{
	{
		DockerNetwork: "internal",
		DefaultPool:   "web",
		DefaultServiceProvider: &ServiceProviderConfig{
			Name:   "vultr",
			Region: "ewr",
			Plan:   "vc2-1c-2gb",
		},
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
				ServiceProvider: &ServiceProviderConfig{
					Name:   "vultr",
					Region: "sgp",
					Plan:   "vc2-2c-4gb",
					Tag:    "web",
				},
			},
		},
	},
	{
		DockerNetwork: "internal",
		DefaultPool:   "inventory",
		DefaultServiceProvider: &ServiceProviderConfig{
			Name:   "vultr",
			Region: "ewr",
			Plan:   "vc2-1c-2gb",
		},
		DefaultPoolConfig: &PoolConfig{
			Min:              1,
			TargetInflight:   2,
			ScaleOutCooldown: 3 * time.Second,
			ScaleInCooldown:  15 * time.Second,
			DrainGrace:       5 * time.Second,
		},
		Rules: []RouteRule{
			{
				Prefix: "/api/catalog",
				Pool:   "catalog",
			},
			{
				Prefix: "/api/inventory",
				Pool:   "inventory",
			},
			{
				Prefix: "/api/checkout",
				Pool:   "checkout",
			},
		},
		Pools: []PoolConfig{
			{
				Name:        "catalog",
				Image:       "traefik/whoami",
				BackendPort: 80,
				HealthPath:  "/",
				Max:         2,
			},
			{
				Name:        "inventory",
				Image:       "traefik/whoami",
				BackendPort: 80,
				HealthPath:  "/",
				Max:         4,
			},
			{
				Name:        "checkout",
				Image:       "traefik/whoami",
				BackendPort: 80,
				HealthPath:  "/",
				Min:         2,
				Max:         12,
			},
		},
	}}

func TestUnmarshalCaddyfile(t *testing.T) {
	if len(sampleCaddyfiles) != len(sampleApps) {
		t.Errorf("samples are inequal: \nsample Caddyfiles %v \nsample Apps: %v",
			len(sampleCaddyfiles),
			len(sampleApps),
		)
	}

	for i, a := range sampleApps {
		app := new(App)

		tokens, err := caddyfile.Tokenize(sampleCaddyfiles[i], "caddyfile")
		if err != nil {
			t.Error(err)
		}

		err = app.UnmarshalCaddyfile(caddyfile.NewDispenser(tokens))
		if err != nil {
			t.Error(err)
		}

		if !reflect.DeepEqual(app, a) {
			t.Errorf("Incorrect Result: \nresult %v \nexpected: %v", app, a)
		}
	}
}
