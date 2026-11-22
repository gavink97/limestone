package limestone

import (
	"errors"
	"fmt"
	"strings"
)

// "unhealthy" members survive only until the next successful probe (~1 tick)
func (a *App) injectTestCondition(pool string, req reconcileRequest) (map[string]any, error) {
	out := map[string]any{"condition": req.Condition}

	switch req.Condition {
	case "unhealthy":
		id, err := a.resolveMember(pool, req.ID)
		if err != nil {
			return nil, err
		}

		a.reg.SetHealth(pool, id, false)
		out["id"] = id

	case "healthy":
		id, err := a.resolveMember(pool, req.ID)
		if err != nil {
			return nil, err
		}

		a.reg.SetHealth(pool, id, true)
		out["id"] = id

	case "draining":
		id, err := a.resolveMember(pool, req.ID)
		if err != nil {
			return nil, err
		}

		a.reg.MarkDraining(pool, id)
		out["id"] = id

	case "remove":
		id, err := a.resolveMember(pool, req.ID)
		if err != nil {
			return nil, err
		}

		a.retireMember(pool, id)
		out["id"] = id

	case "inflight":
		if req.InflightDemand < 0 {
			req.InflightDemand = 0
		}

		a.reg.Inflight(pool).Store(int64(req.InflightDemand))
		out["value"] = req.InflightDemand

	default:
		return nil, fmt.Errorf("unknown condition %q (want unhealthy, healthy, draining, remove or inflight)", req.Condition)
	}

	return out, nil
}

func (a *App) resolveMember(pool, ref string) (string, error) {
	if ref == "" {
		return "", errors.New("member id is required for this condition")
	}

	var hit string
	for _, m := range a.reg.Members(pool) {
		if m.ID == ref {
			return m.ID, nil
		}

		if strings.HasPrefix(m.ID, ref) {
			if hit != "" {
				return "", fmt.Errorf("member id %q is ambiguous in pool %q", ref, pool)
			}

			hit = m.ID
		}
	}
	if hit == "" {
		return "", fmt.Errorf("no member matches %q in pool %q", ref, pool)
	}

	return hit, nil
}
