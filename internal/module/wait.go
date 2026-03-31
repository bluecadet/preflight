package module

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"
)

// WaitModule polls until a condition is met or a timeout expires.
// Params:
//   - condition (required): "port_open", "file_exists", or "service_running"
//   - target (required): address:port / path / service name depending on condition
//   - timeout: duration string (default "5m")
type WaitModule struct{}

func (m *WaitModule) Check(_ context.Context, params map[string]interface{}) (bool, error) {
	condition, err := paramStringRequired(params, "condition")
	if err != nil {
		return false, err
	}
	tgt, err := paramStringRequired(params, "target")
	if err != nil {
		return false, err
	}

	met, err := checkCondition(condition, tgt)
	if err != nil {
		return false, err
	}
	// needsChange = condition not yet met
	return !met, nil
}

func (m *WaitModule) Apply(ctx context.Context, params map[string]interface{}) error {
	condition, err := paramStringRequired(params, "condition")
	if err != nil {
		return err
	}
	tgt, err := paramStringRequired(params, "target")
	if err != nil {
		return err
	}
	timeoutStr, err := paramString(params, "timeout", "5m")
	if err != nil {
		return err
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		return fmt.Errorf("wait: invalid timeout %q: %w", timeoutStr, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		met, err := checkCondition(condition, tgt)
		if err != nil {
			return err
		}
		if met {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait: timeout after %s waiting for condition %q on %q", timeoutStr, condition, tgt)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
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
		conn.Close()
		return true, nil

	case "service_running":
		return checkServiceRunning(tgt)

	default:
		return false, fmt.Errorf("wait: unknown condition %q (want port_open|file_exists|service_running)", condition)
	}
}
