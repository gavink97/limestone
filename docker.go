package limestone

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

const (
	lblOwner = "limestone.owner"
	lblPool  = "limestone.pool"
)

type Instance struct {
	ID, Dial string
}

type DockerProvisioner struct {
	cli          *client.Client
	network      string
	owner        string
	spawnTimeout time.Duration
	probe        *http.Client
}

// moby doesn't write a response on api calls, maybe a good first issue for moby
func (p *DockerProvisioner) Start(ctx context.Context, pc PoolConfig) (Instance, error) {
	ctx, cancel := context.WithTimeout(ctx, p.spawnTimeout)
	defer cancel()

	name := fmt.Sprintf("limestone-%s-%s", pc.Name, randHex(4))
	ctr, err := p.cli.ContainerCreate(ctx,
		client.ContainerCreateOptions{
			Name: name,
			Config: &container.Config{
				Image:  pc.Image,
				Labels: map[string]string{lblOwner: p.owner, lblPool: pc.Name},
			},
			NetworkingConfig: &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					p.network: {},
				},
			},
		})

	if err != nil {
		return Instance{}, fmt.Errorf("create %s: %w", name, err)
	}

	_, err = p.cli.ContainerStart(ctx, ctr.ID, client.ContainerStartOptions{})
	if err != nil {
		p.forceRemove(ctr.ID)
		return Instance{}, fmt.Errorf("start %s: %w", name, err)
	}

	dial, err := p.waitForDial(ctx, ctr.ID, pc)
	if err != nil {
		p.forceRemove(ctr.ID)
		return Instance{}, err
	}

	if err := p.probeOnce(ctx, dial, pc.HealthPath); err != nil {
		p.forceRemove(ctr.ID)
		return Instance{}, fmt.Errorf("%s unhealthy after start: %w", name, err)
	}

	return Instance{ID: ctr.ID[:12], Dial: dial}, nil
}

func (p *DockerProvisioner) waitForDial(ctx context.Context, id string, pc PoolConfig) (string, error) {
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		info, err := p.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
		if err != nil {
			return "", err
		}

		ep := info.Container.NetworkSettings.Networks[p.network]
		if ep != nil && ep.IPAddress.String() != "" {
			return net.JoinHostPort(ep.IPAddress.String(), strconv.Itoa(pc.BackendPort)), nil
		}

		time.Sleep(200 * time.Millisecond)
	}
}

func (p *DockerProvisioner) probeOnce(ctx context.Context, dial, path string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+dial+path, nil)
		resp, err := p.probe.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
}

func (p *DockerProvisioner) Stop(ctx context.Context, id string) error {
	t := 5
	_, err := p.cli.ContainerStop(ctx, id, client.ContainerStopOptions{
		Timeout: &t,
	})

	if err != nil {
		return err
	}

	_, err = p.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})

	return err
}

type Orphan struct {
	Instance
	Pool string
}

func (p *DockerProvisioner) Adopt(ctx context.Context, knownPools map[string]*poolState) ([]Orphan, error) {
	filter := make(client.Filters).Add("label", fmt.Sprintf("%s=%s", lblOwner, p.owner))

	list, err := p.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filter,
	})

	if err != nil {
		return nil, err
	}

	var out []Orphan

	for _, c := range list.Items {
		pc, known := knownPools[c.Labels[lblPool]]
		if !known || c.State != "running" {
			p.forceRemove(c.ID)
			continue
		}

		info, err := p.cli.ContainerInspect(ctx, c.ID, client.ContainerInspectOptions{})
		if err != nil {
			continue
		}

		ep := info.Container.NetworkSettings.Networks[p.network]
		if ep == nil || ep.IPAddress.String() == "" {
			p.forceRemove(c.ID)
			continue
		}

		out = append(out, Orphan{
			Instance: Instance{ID: c.ID, Dial: net.JoinHostPort(ep.IPAddress.String(), strconv.Itoa(pc.cfg.BackendPort))},
			Pool:     pc.cfg.Name,
		})
	}
	return out, nil
}

func (p *DockerProvisioner) forceRemove(id string) {
	_, err := p.cli.ContainerRemove(context.Background(), id, client.ContainerRemoveOptions{
		Force: true,
	})

	if err != nil {
		caddy.Log().Error(err.Error())
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}
