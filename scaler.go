package limestone

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"
)

func (a *App) controlLoop(ctx context.Context) {
	tick := time.NewTicker(time.Duration(a.TickInterval))
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			a.reg.Broadcast()
			for _, st := range a.states {
				go a.reconcile(ctx, st)
			}
		case pool := <-a.demand:
			if st, ok := a.states[pool]; ok {
				go a.reconcile(ctx, st)
			}
		}
	}
}

func (a *App) retireMember(pool, id string) {
	a.reg.RemoveMember(pool, id)

	go func() {
		if err := a.prov.Stop(context.Background(), id); err != nil {
			a.logger.Warn("stop failed", zap.String("pool", pool), zap.String("id", id), zap.Error(err))
		}
	}()
}

func (a *App) reconcile(ctx context.Context, st *poolState) {
	if !st.busy.CompareAndSwap(false, true) {
		return
	}

	defer st.busy.Store(false)
	pool := st.cfg.Name

	for _, m := range a.reg.Members(pool) {
		if m.Draining {
			continue
		}

		go a.probeMember(st.cfg, m)
	}

	for _, m := range a.reg.Members(pool) {
		if (m.Draining || !m.Healthy) && time.Since(m.LastChange) > st.cfg.DrainGrace {
			a.retireMember(pool, m.ID)
		}
	}

	healthy := a.reg.HealthyCount(pool)

	if healthy < st.cfg.Min {
		for i := 0; i < st.cfg.Min-healthy; i++ {
			a.spawnOne(ctx, st)
		}

		return
	}
	if healthy > st.cfg.Max {
		for i := 0; i < healthy-st.cfg.Max; i++ {
			a.drainOne(st)
		}

		return
	}

	inflight := a.reg.Inflight(pool).Load()
	perReplica := float64(inflight) / float64(max(1, healthy))

	switch {
	case perReplica >= float64(st.cfg.TargetInflight) && healthy < st.cfg.Max:
		if time.Since(st.lastScale) >= st.cfg.ScaleOutCooldown {
			a.spawnOne(ctx, st)
		}
	case perReplica*2 <= float64(st.cfg.TargetInflight) && healthy > st.cfg.Min:
		if time.Since(st.lastScale) >= st.cfg.ScaleInCooldown {
			a.drainOne(st)
		}
	}
}

func (a *App) spawnOne(ctx context.Context, st *poolState) {
	st.lastScale = time.Now()
	spawnCtx, cancel := context.WithTimeout(ctx, time.Duration(a.SpawnTimeout))
	defer cancel()

	inst, err := a.prov.Start(spawnCtx, st.cfg)
	if err != nil {
		a.logger.Error("provision failed", zap.String("pool", st.cfg.Name), zap.Error(err))
		return
	}

	a.reg.AddMember(st.cfg.Name, inst.ID, inst.Dial, true)

	a.logger.Info(
		"instance up",
		zap.String("pool", st.cfg.Name),
		zap.String("dial", inst.Dial),
		zap.String("id", inst.ID),
	)
}

func (a *App) drainOne(st *poolState) {
	st.lastScale = time.Now()

	for _, m := range a.reg.Members(st.cfg.Name) {
		if m.Healthy && !m.Draining {
			a.reg.MarkDraining(st.cfg.Name, m.ID)
			a.logger.Info("draining instance", zap.String("pool", st.cfg.Name), zap.String("id", m.ID))
			return
		}
	}
}

func (a *App) probeMember(pc PoolConfig, m MemberInfo) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+m.Dial+pc.HealthPath, nil)
	if err != nil {
		a.reg.SetHealth(pc.Name, m.ID, false)
		return
	}

	resp, err := a.probeClient.Do(req)
	if err != nil {
		a.reg.SetHealth(pc.Name, m.ID, false)
		return
	}

	defer resp.Body.Close() //nolint:errcheck
	a.reg.SetHealth(pc.Name, m.ID, resp.StatusCode == http.StatusOK)
}
