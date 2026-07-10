# Source this in a VHS tape (or an interactive shell) to render a simulated
# `preflight apply` using the offline simulator at tools/sim.
#
# The simulator feeds fabricated task events into the *real* internal/output
# TUI renderer, so the on-screen output is identical to a live run. This lets
# the README header GIF demonstrate an apply without any live hosts.
#
# Only `apply` is intercepted; every other subcommand falls through to the real
# preflight binary. Set PREFLIGHT_SIM_BIN to a pre-built binary for an instant
# on-screen start (the tape pre-builds one at /tmp/preflight-sim); leave it
# unset to use `go run` (warm build cache makes this ~instant too).

PREFLIGHT_SIM_BIN="${PREFLIGHT_SIM_BIN:-go run ./tools/sim}"

preflight() {
  if [ "$1" = "apply" ]; then
    # Intentionally unquoted: PREFLIGHT_SIM_BIN may be `go run ./tools/sim`.
    # shellcheck disable=SC2086
    $PREFLIGHT_SIM_BIN readme --format tui --delay 1200ms
  else
    command preflight "$@"
  fi
}
