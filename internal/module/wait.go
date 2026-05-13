package module

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/bluecadet/preflight/internal/target"
)

type WaitParams struct {
	Condition string `param:"condition,required"`
	Target    string `param:"target,required"`
	Timeout   string `param:"timeout" default:"5m"`
}

type WaitModule struct{}

func (m *WaitModule) Check(_ context.Context, params map[string]any, _ target.OutputFunc) (target.CheckResult, error) {
	var p WaitParams
	if err := Decode(params, &p); err != nil {
		return target.CheckResult{}, err
	}

	met, err := checkCondition(p.Condition, p.Target)
	if err != nil {
		return target.CheckResult{}, err
	}
	return target.CheckResult{NeedsChange: !met}, nil
}

func (m *WaitModule) Apply(ctx context.Context, params map[string]any, _ target.OutputFunc) (target.ApplyResult, error) {
	var p WaitParams
	if err := Decode(params, &p); err != nil {
		return target.ApplyResult{}, err
	}

	timeout, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return target.ApplyResult{}, fmt.Errorf("wait: invalid timeout %q: %w", p.Timeout, err)
	}

	deadline := time.Now().Add(timeout)
	pollTimer := time.NewTimer(0)
	defer pollTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			return target.ApplyResult{}, ctx.Err()
		case <-pollTimer.C:
		}
		met, err := checkCondition(p.Condition, p.Target)
		if err != nil {
			return target.ApplyResult{}, err
		}
		if met {
			return target.ApplyResult{}, nil
		}
		if time.Now().After(deadline) {
			return target.ApplyResult{}, fmt.Errorf("wait: timeout after %s waiting for condition %q on %q", p.Timeout, p.Condition, p.Target)
		}
		pollTimer.Reset(5 * time.Second)
	}
}

func checkCondition(condition, tgt string) (bool, error) {
	switch condition {
	case "file_exists":
		_, err := os.Stat(tgt)
		if err == nil {
			return true, nil
		}
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("wait: stat %q: %w", tgt, err)

	case "port_open":
		conn, err := net.DialTimeout("tcp", tgt, 2*time.Second)
		if err != nil {
			return false, nil //nolint:nilerr // connection refused = not yet open
		}
		_ = conn.Close()
		return true, nil

	case "service_running":
		return checkServiceRunning(tgt)

	default:
		return false, fmt.Errorf("wait: unknown condition %q (want port_open|file_exists|service_running)", condition)
	}
}
