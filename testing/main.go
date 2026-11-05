package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

var uri = "http://localhost:2019/limestone"

var CheckInflight bool
var Verbose bool
var Logger *slog.Logger

var ScaleOutCooldown time.Duration = 3
var ScaleInCooldown time.Duration = 15
var Wait = time.Duration(100) * time.Millisecond

func main() {
	flag.BoolVar(&CheckInflight, "inflight", false, "enables inflight testing")
	flag.BoolVar(&Verbose, "verbose", false, "enables verbose output")
	flag.Parse()

	logLevel := slog.LevelWarn

	if Verbose {
		logLevel = slog.LevelInfo
	}

	Logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))

	err := checkServerStatus()
	if err != nil {
		log.Fatal(err)
	}

	err = checkAdminAPI()
	if err != nil {
		log.Fatal(err)
	}

	err = InjectionTesting()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("injection test complete")
}

func checkServerStatus() error {
	out, err := exec.Command("docker", "ps").Output()
	if err != nil {
		return err
	}

	if !strings.Contains(string(out), "limestone") {
		return errors.New("limestone container not running")
	}

	return nil
}

func checkAdminAPI() error {
	resp, err := http.Get(fmt.Sprint(uri, "/pools"))
	if err != nil {
		return err
	}

	if resp.StatusCode > 200 {
		return errors.New("admin API is disabled or not 0.0.0.0:2019")
	}

	return nil
}

func InjectionTesting() error {
	err := TestReconcilerInjection()
	if err != nil {
		return err
	}

	return nil
}

func TestReconcilerInjection() error {
	pools, err := getPools()
	if err != nil {
		return err
	}

	var defaultPool string
	for _, pool := range pools.Pools {
		if pool.DefaultPool {
			defaultPool = pool.Name
		}
	}

	Logger.Info(fmt.Sprintf("default pool = %s", defaultPool))

	var members *membersResponse
	var target member

	members, err = getMembers(defaultPool)
	if err != nil {
		return err
	}

	target = members.Members[0]

	printPoolMembers(members.Members, defaultPool)
	Logger.Info(
		"setting member to unhealthy",
		slog.String("stage", "unhealthy"),
		slog.String("member", target.ID),
	)

	err = newReconcileRequest(reconcileParams{
		Condition: "unhealthy",
		ID:        target.ID,
	}).Send(defaultPool)
	if err != nil {
		return err
	}

	members, err = getMembers(defaultPool)
	if err != nil {
		return err
	}

	for (len(members.Members)) < 2 {
		time.Sleep(Wait)

		members, err = getMembers(defaultPool)
		if err != nil {
			return err
		}
	}

	printPoolMembers(members.Members, defaultPool)
	// it is correct behavior that target is healthy on print as we're making it unhealthy for one tick to trigger a scaleout

	Logger.Info("awaiting pool scale in", slog.String("stage", "unhealthy"))
	time.Sleep(time.Duration(25) * time.Second)

	members, err = getMembers(defaultPool)
	if err != nil {
		return err
	}

	printPoolMembers(members.Members, defaultPool)

	target = members.Members[0]
	Logger.Info(
		"setting member to draining",
		slog.String("stage", "drain"),
		slog.String("member", target.ID),
	)

	err = newReconcileRequest(reconcileParams{
		Condition: "draining",
		ID:        target.ID,
	}).Send(defaultPool)
	if err != nil {
		return err
	}

	members, err = getMembers(defaultPool)
	if err != nil {
		return err
	}

	for (len(members.Members)) < 2 {
		time.Sleep(Wait)

		members, err = getMembers(defaultPool)
		if err != nil {
			return err
		}
	}

	printPoolMembers(members.Members, defaultPool)
	Logger.Info(
		"awaiting member retirement",
		slog.String("stage", "drain"),
		slog.String("member", target.ID),
	)

	time.Sleep(ScaleInCooldown * time.Second)

	members, err = getMembers(defaultPool)
	if err != nil {
		return err
	}

	target = members.Members[0]

	printPoolMembers(members.Members, defaultPool)
	Logger.Info(
		"removing member",
		slog.String("stage", "remove"),
		slog.String("member", target.ID),
	)

	err = newReconcileRequest(reconcileParams{
		Condition: "remove",
		ID:        target.ID,
	}).Send(defaultPool)
	if err != nil {
		return err
	}

	members, err = getMembers(defaultPool)
	if err != nil {
		return err
	}

	for (len(members.Members)) < 1 {
		time.Sleep(Wait)

		members, err = getMembers(defaultPool)
		if err != nil {
			return err
		}
	}

	printPoolMembers(members.Members, defaultPool)

	if !CheckInflight {
		Logger.Warn("skipping Inflight injection check")
		return nil
	}

	Logger.Info("filling pool", slog.String("stage", "inflight-fill"))
	err = newReconcileRequest(reconcileParams{
		Condition:      "inflight",
		InflightDemand: 4,
	}).Send(defaultPool)
	if err != nil {
		return err
	}

	Logger.Info("draining pool", slog.String("stage", "inflight-drain"))
	err = newReconcileRequest(reconcileParams{
		Condition:      "inflight",
		InflightDemand: 0,
	}).Send(defaultPool)
	if err != nil {
		return err
	}

	return nil
}

func printPoolMembers(members []member, pool string) {
	Logger.Info("printing members in pool", slog.String("pool", pool))

	for i, m := range members {
		Logger.Info(fmt.Sprintf("member %d", i),
			slog.String("id", m.ID),
			slog.Bool("draining", m.Draining),
			slog.Bool("healthy", m.Healthy),
		)
	}
}

type pool struct {
	Name        string `json:"name"`
	MemberCount int    `json:"member_count"`
	DefaultPool bool   `json:"default_pool"`
}

type poolsResponse struct {
	Pools []pool `json:"pools"`
}

type member struct {
	ID         string    `json:"id"`
	Dial       string    `json:"dial"`
	Healthy    bool      `json:"healthy"`
	Draining   bool      `json:"draining"`
	LastChange time.Time `json:"last_change"`
}

type membersResponse struct {
	Inflight int      `json:"inflight"`
	Members  []member `json:"members"`
}

func getPools() (*poolsResponse, error) {
	body, err := get(fmt.Sprint(uri, "/pools"))
	if err != nil {
		return nil, err
	}

	var pools poolsResponse
	err = json.Unmarshal(body, &pools)
	if err != nil {
		return nil, err
	}

	return &pools, nil
}

func getMembers(pool string) (*membersResponse, error) {
	body, err := get(fmt.Sprint(uri, "/members/", pool))
	if err != nil {
		return nil, err
	}

	var members membersResponse

	err = json.Unmarshal(body, &members)
	if err != nil {
		return nil, err
	}

	return &members, nil
}

func get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

type reconcileRequest struct {
	Condition      string `json:"condition"`
	ID             string `json:"id,omitempty"`
	InflightDemand int    `json:"inflight_demand,omitempty"`
}

type reconcileParams struct {
	Condition      string
	ID             string
	InflightDemand int
}

func newReconcileRequest(params reconcileParams) *reconcileRequest {
	return &reconcileRequest{
		Condition:      params.Condition,
		ID:             params.ID,
		InflightDemand: params.InflightDemand,
	}
}

type reconcileResponse struct {
	Request reconcileRequest `json:"request"`
	State   membersResponse  `json:"state"`
}

func (req *reconcileRequest) Send(pool string) error {
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}

	body := bytes.NewBuffer(b)
	resp, err := http.Post(fmt.Sprint(uri, "/test/reconciler/", pool), "application/json", body)
	if err != nil {
		return err
	}

	if resp.StatusCode > 200 {
		return fmt.Errorf("%s request on member %s failed", req.Condition, req.ID)
	}

	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var r reconcileResponse
	err = json.Unmarshal(respBody, &r)
	if err != nil {
		return err
	}

	// it seems that sometimes this r state member is out of date? or is it that items in the pool that are unhealthy are not pruned

	switch req.Condition {
	case "unhealthy":
		if r.State.Members[0].Healthy {
			return fmt.Errorf("member health not in correct state: %v. expected: %v",
				r.State.Members[0].Healthy, false)
		}
	case "healthy":
		if !r.State.Members[0].Healthy {
			return fmt.Errorf("member health not in correct state: %v. expected: %v",
				r.State.Members[0].Healthy, true)
		}
	case "draining":
		if !r.State.Members[0].Draining {
			return fmt.Errorf("member draining not in correct state: %v. expected: %v",
				r.State.Members[0].Healthy, true)
		}
	case "remove":
		if len(r.State.Members) > 0 {
			return fmt.Errorf("expected 0 members. members present: %d", len(r.State.Members))
		}
	case "inflight":
		if req.InflightDemand == 0 {
			time.Sleep(time.Duration(len(r.State.Members)) * ScaleInCooldown * time.Second)

		} else {
			time.Sleep(time.Duration(req.InflightDemand-1) * ScaleOutCooldown * time.Second)

		}

		members, err := getMembers(pool)
		if err != nil {
			return err
		}

		printPoolMembers(members.Members, pool)

		if req.InflightDemand == 0 {
			if len(members.Members) != 1 {
				return fmt.Errorf("expected 1 member. members present: %d", len(members.Members))
			}
		} else {
			if len(members.Members) != 3 {
				return fmt.Errorf("expected 3 member. members present: %d", len(members.Members))
			}
		}

	default:
		return fmt.Errorf("unknown request condition: %s", req.Condition)
	}

	return nil
}
