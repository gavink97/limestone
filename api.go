package limestone

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
)

func init() { caddy.RegisterModule(new(AdminAPI)) }

type AdminAPI struct {
	app *App
}

func (AdminAPI) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "admin.api.limestone",
		New: func() caddy.Module { return new(AdminAPI) },
	}
}

func (a *AdminAPI) Provision(ctx caddy.Context) error {
	appIface, err := ctx.App("limestone")
	if err != nil {
		return err
	}

	app, ok := appIface.(*App)
	if !ok {
		return fmt.Errorf("limestone app is not *App")
	}

	a.app = app

	return nil
}

func (a *AdminAPI) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{
		{Pattern: "GET /limestone/pools", Handler: caddy.AdminHandlerFunc(a.handlePools)},
		{Pattern: "GET /limestone/members/{pool}", Handler: caddy.AdminHandlerFunc(a.handleMembers)},
		{Pattern: "POST /limestone/test/reconciler/{pool}", Handler: caddy.AdminHandlerFunc(a.handleReconciler)},
	}
}

type reconcileRequest struct {
	ID             string `json:"id"`
	Condition      string `json:"condition"`
	InflightDemand int    `json:"inflight_demand"`
}

func (a *AdminAPI) handlePools(w http.ResponseWriter, r *http.Request) error {
	return writeResponse(w, a.poolSnapshot())
}

func (a *AdminAPI) handleMembers(w http.ResponseWriter, r *http.Request) error {
	pool := r.PathValue("pool")

	_, ok := a.app.states[pool]
	if !ok {
		return caddy.APIError{HTTPStatus: http.StatusNotFound,
			Message: "unknown pool"}
	}

	return writeResponse(w, a.membersSnapshot(pool))
}

// maybe perform a scatter gather to get data from each member of the pool?

func (a *AdminAPI) handleReconciler(w http.ResponseWriter, r *http.Request) error {
	defer r.Body.Close() //nolint:errcheck

	pool := r.PathValue("pool")
	_, ok := a.app.states[pool]
	if !ok {
		return caddy.APIError{HTTPStatus: http.StatusNotFound,
			Message: "unknown pool"}
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return caddy.APIError{HTTPStatus: http.StatusBadRequest,
			Message: fmt.Sprintf("reading body: %v", err)}
	}

	var req reconcileRequest
	err = json.Unmarshal(body, &req)
	if err != nil {
		return caddy.APIError{HTTPStatus: http.StatusBadRequest,
			Message: fmt.Sprintf("invalid JSON: %v", err)}
	}

	if req.Condition == "" {
		return caddy.APIError{HTTPStatus: http.StatusBadRequest,
			Message: "condition is required: unhealthy | healthy | draining | remove | inflight"}
	}

	injected, err := a.app.injectTestCondition(pool, req)
	if err != nil {
		return caddy.APIError{HTTPStatus: http.StatusBadRequest, Message: err.Error()}
	}

	a.app.reg.Nudge(pool)

	return writeResponse(w, map[string]any{
		"request": injected,
		"state":   a.membersSnapshot(pool),
	})
}

func writeResponse(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func (a *AdminAPI) poolSnapshot() map[string]any {
	pools := a.app.reg.pools
	out := make([]map[string]any, 0, len(pools))

	for p := range pools {
		var defaultpool bool

		if strings.EqualFold(p, a.app.DefaultPool) {
			defaultpool = true
		}

		out = append(out, map[string]any{
			"name":         p,
			"member_count": len(a.app.reg.Members(p)),
			"default_pool": defaultpool,
		})
	}

	return map[string]any{
		"pools": out,
	}
}

func (a *AdminAPI) membersSnapshot(pool string) map[string]any {
	members := a.app.reg.Members(pool)
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, map[string]any{
			"id":          m.ID,
			"dial":        m.Dial,
			"healthy":     m.Healthy,
			"draining":    m.Draining,
			"last_change": m.LastChange.UTC().Format(time.RFC3339Nano),
		})
	}
	return map[string]any{
		"inflight": a.app.reg.Inflight(pool).Load(),
		"members":  out,
	}
}
