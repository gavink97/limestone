package limestone

import (
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func (a *App) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for nesting := d.Nesting(); d.NextBlock(nesting); {
			switch d.Val() {
			case "docker_network":
				if !d.NextArg() {
					return d.ArgErr()
				}

				a.DockerNetwork = d.Val()

			case "default_pool":
				if !d.NextArg() {
					return d.ArgErr()
				}

				a.DefaultPool = d.Val()

			case "default_service_provider":
				spc := NewServiceProviderConfig(ServiceProviderConfigParams{})

				for sub := d.Nesting(); d.NextBlock(sub); {
					if err := parseServiceProvider(spc, d); err != nil {
						return err
					}
				}

				a.DefaultServiceProvider = spc

			case "default_pool_config":
				pc := PoolConfig{}

				for sub := d.Nesting(); d.NextBlock(sub); {
					if err := parsePoolOption(&pc, d); err != nil {
						return err
					}
				}

				a.DefaultPoolConfig = &pc

			case "rules":
				for sub := d.Nesting(); d.NextBlock(sub); {
					r := RouteRule{Prefix: d.Val()}

					if !d.NextArg() {
						return d.ArgErr()
					}

					r.Pool = d.Val()
					a.Rules = append(a.Rules, r)
				}

			case "pool":
				if !d.NextArg() {
					return d.ArgErr()
				}

				pc := PoolConfig{Name: d.Val()}

				for sub := d.Nesting(); d.NextBlock(sub); {
					if err := parsePoolOption(&pc, d); err != nil {
						return err
					}
				}

				a.Pools = append(a.Pools, pc)

			default:
				return d.Errf("unknown limestone option: %s", d.Val())
			}

		}
	}

	return nil
}

func parsePoolOption(pc *PoolConfig, d *caddyfile.Dispenser) error {
	switch d.Val() {
	case "image":
		if !d.NextArg() {
			return d.ArgErr()
		}

		pc.Image = d.Val()

	case "backend_port":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := strconv.Atoi(d.Val())
		if err != nil {
			return d.Errf("bad number: %v", err)
		}

		pc.BackendPort = v

	case "health_path":
		if !d.NextArg() {
			return d.ArgErr()
		}

		pc.HealthPath = d.Val()

	case "min":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := strconv.Atoi(d.Val())
		if err != nil {
			return d.Errf("bad number: %v", err)
		}

		pc.Min = v

	case "max":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := strconv.Atoi(d.Val())
		if err != nil {
			return d.Errf("bad number: %v", err)
		}

		pc.Max = v

	case "target_inflight":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := strconv.Atoi(d.Val())
		if err != nil {
			return d.Errf("bad number: %v", err)
		}

		pc.TargetInflight = v

	case "scale_out_cooldown":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := caddy.ParseDuration(d.Val())
		if err != nil {
			return d.Errf("bad duration: %v", err)
		}

		pc.ScaleOutCooldown = v

	case "scale_in_cooldown":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := caddy.ParseDuration(d.Val())
		if err != nil {
			return d.Errf("bad duration: %v", err)
		}

		pc.ScaleInCooldown = v

	case "drain_grace":
		if !d.NextArg() {
			return d.ArgErr()
		}

		v, err := caddy.ParseDuration(d.Val())
		if err != nil {
			return d.Errf("bad duration: %v", err)
		}

		pc.DrainGrace = v

	case "service_provider":
		spc := NewServiceProviderConfig(ServiceProviderConfigParams{
			Tag: pc.Name,
		})

		for sub := d.Nesting(); d.NextBlock(sub); {
			if err := parseServiceProvider(spc, d); err != nil {
				return err
			}
		}

		pc.ServiceProvider = spc

	default:
		return d.Errf("unknown pool option: %s", d.Val())
	}

	return nil
}

func parseServiceProvider(spc *ServiceProviderConfig, d *caddyfile.Dispenser) error {
	switch d.Val() {
	case "name":
		if !d.NextArg() {
			return d.ArgErr()
		}

		spc.Name = d.Val()

	case "region":
		if !d.NextArg() {
			return d.ArgErr()
		}

		spc.Region = d.Val()

	case "plan":
		if !d.NextArg() {
			return d.ArgErr()
		}

		spc.Plan = d.Val()

	default:
		return d.Errf("unknown infrastructure option: %s", d.Val())

	}

	return nil
}

// these are required otherwise caddy throws out an error:
// module http.reverse_proxy.upstreams.limestone_pool is not a Caddyfile unmarshaler
func (ps *PoolSource) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}

		for nesting := d.Nesting(); d.NextBlock(nesting); {
			return d.Errf("unknown option: %s", d.Val())
		}
	}

	return nil
}

func (h *Handler) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		if d.NextArg() {
			return d.ArgErr()
		}

		for nesting := d.Nesting(); d.NextBlock(nesting); {
			return d.Errf("unknown option: %s", d.Val())
		}
	}

	return nil
}
