package target

import (
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
)

// defaultSSHTimeout is the connection/handshake timeout used when SSHConfig's
// Timeout field is left at its zero value.
const defaultSSHTimeout = 30 * time.Second

var defaultSSHRunnerFactory sshRunnerFactory = func(cfg SSHConfig) (sshRunner, error) {
	if cfg.Jump != nil {
		return dialSSHRunnerViaJump(cfg)
	}
	client, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}
	return newSSHClientRunner(client, nil), nil
}

// dialSSHClient builds an *ssh.ClientConfig from cfg and dials cfg's
// Host:Port directly (defaulting the port to 22 when unset), bounding both
// the TCP connect and the SSH handshake by the config's effective timeout.
func dialSSHClient(cfg SSHConfig) (*ssh.Client, error) {
	clientConfig, agentCloser, err := buildSSHClientConfig(cfg)
	defer closeAgent(agentCloser)
	if err != nil {
		return nil, err
	}
	return dialSSHClientBounded(sshAddr(cfg), clientConfig, clientConfig.Timeout)
}

// sshAddr formats cfg's Host:Port as a dial address, defaulting Port to 22
// when unset.
func sshAddr(cfg SSHConfig) string {
	port := cfg.Port
	if port == 0 {
		port = 22
	}
	return fmt.Sprintf("%s:%d", cfg.Host, port)
}

// dialSSHConnBounded dials addr over TCP and performs the SSH handshake,
// bounding both phases by timeout.
//
// This works around a gap in x/crypto/ssh's own ssh.Dial: it applies
// config.Timeout only to the net.DialTimeout call, leaving the handshake
// itself (ssh.NewClientConn) completely unbounded. A remote that accepts the
// TCP connection but never speaks (or stalls mid-handshake) would otherwise
// hang ssh.Dial forever. Here, the TCP conn's own deadline is used to bound
// the handshake too, then cleared before the conn is handed off to the
// caller for normal use.
func dialSSHConnBounded(addr string, config *ssh.ClientConfig, timeout time.Duration) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error) {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return nil, nil, nil, err
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		// ssh.NewClientConn closes conn itself on handshake failure; closing
		// it again here would be a double close.
		return nil, nil, nil, err
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = sshConn.Close()
		return nil, nil, nil, err
	}
	return sshConn, chans, reqs, nil
}

// dialSSHClientBounded dials addr and completes an SSH handshake using
// config, wrapping the result as an *ssh.Client. Both phases are bounded by
// timeout; see dialSSHConnBounded.
func dialSSHClientBounded(addr string, config *ssh.ClientConfig, timeout time.Duration) (*ssh.Client, error) {
	sshConn, chans, reqs, err := dialSSHConnBounded(addr, config, timeout)
	if err != nil {
		return nil, err
	}
	return ssh.NewClient(sshConn, chans, reqs), nil
}

// dialSSHViaBastionBounded builds an *ssh.ClientConfig from targetCfg, then
// opens a channel to targetCfg's Host:Port through bastionClient and performs
// the second SSH handshake, bounded by targetCfg's effective timeout.
//
// Unlike the first hop, the net.Conn returned by (*ssh.Client).Dial (an
// in-tunnel channel conn) does not support SetDeadline — it always returns
// an error from SetDeadline — so dialSSHConnBounded's deadline technique
// cannot be used here. Instead, the channel-open and handshake run in a
// goroutine that reports its result on a buffered channel, raced against a
// timer. On timeout, bastionClient is closed: this tears down the tunneled
// channel out from under the goroutine's blocked Dial/handshake call, so it
// returns (with an error) instead of leaking forever, even though this
// function has already returned.
func dialSSHViaBastionBounded(bastionClient *ssh.Client, targetCfg SSHConfig, bastionAddr string) (*ssh.Client, error) {
	config, agentCloser, err := buildSSHClientConfig(targetCfg)
	defer closeAgent(agentCloser)
	if err != nil {
		return nil, err
	}

	targetAddr := sshAddr(targetCfg)
	timeout := config.Timeout

	type dialResult struct {
		client *ssh.Client
		err    error
	}
	done := make(chan dialResult, 1)

	go func() {
		conn, err := bastionClient.Dial("tcp", targetAddr)
		if err != nil {
			done <- dialResult{err: fmt.Errorf("ssh: dial target %s via jump host %s: %w", targetAddr, bastionAddr, err)}
			return
		}
		sshConn, chans, reqs, err := ssh.NewClientConn(conn, targetAddr, config)
		if err != nil {
			// ssh.NewClientConn closes conn itself on handshake failure.
			done <- dialResult{err: fmt.Errorf("ssh: dial target %s via jump host %s: %w", targetAddr, bastionAddr, err)}
			return
		}
		done <- dialResult{client: ssh.NewClient(sshConn, chans, reqs)}
	}()

	select {
	case r := <-done:
		return r.client, r.err
	case <-time.After(timeout):
		_ = bastionClient.Close()
		return nil, fmt.Errorf("ssh: dial target %s via jump host %s: timeout after %s", targetAddr, bastionAddr, timeout)
	}
}

// dialSSHRunnerViaJump dials cfg.Host through the single-hop bastion
// described by cfg.Jump (an SSH ProxyJump): it connects to the jump host
// first, then tunnels a second SSH handshake to the real target over that
// connection. The bastion and target each use their own, independent
// SSHConfig (auth, host-key policy, timeout) — the target does not inherit
// anything from the jump host's configuration. Both hops are bounded: the
// bastion hop by dialSSHClient (TCP connect + handshake), and the target hop
// by dialSSHViaBastionBounded (channel-open + handshake, since the tunneled
// channel conn cannot use SetDeadline directly).
func dialSSHRunnerViaJump(cfg SSHConfig) (sshRunner, error) {
	jumpCfg := *cfg.Jump
	if jumpCfg.Jump != nil {
		return nil, fmt.Errorf("ssh: jump host %s: only a single jump hop is supported (nested jump hosts are not allowed)", jumpCfg.Host)
	}

	bastionAddr := sshAddr(jumpCfg)

	bastionClient, err := dialSSHClient(jumpCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh: dial jump host %s: %w", bastionAddr, err)
	}

	targetClient, err := dialSSHViaBastionBounded(bastionClient, cfg, bastionAddr)
	if err != nil {
		_ = bastionClient.Close()
		return nil, err
	}

	return newSSHClientRunner(targetClient, bastionClient), nil
}
