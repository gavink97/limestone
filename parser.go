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

	default:
		return d.Errf("unknown pool option: %s", d.Val())
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
