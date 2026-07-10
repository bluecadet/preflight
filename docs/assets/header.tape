# README header GIF for preflight.
#
# Renders a simulated `preflight apply` using the offline simulator
# (tools/sim), which feeds fabricated task events into the *real* TUI
# renderer. No live hosts are required. See docs/assets/sim-preflight.sh.
#
#   vhs docs/assets/header.tape
#
# Rebuilds the simulator into /tmp/preflight-sim during a hidden setup step
# so the on-screen run starts instantly.

Output docs/assets/header.gif

Set Shell "bash"
Set FontSize 22
Set FontFamily "JetBrains Mono"
Set Width 1300
Set Height 800
Set Padding 24
Set WindowBarSize 24
Set TypingSpeed 35ms

# Hidden setup: pre-build the simulator and install the preflight shim that
# routes `apply` to it. These steps don't appear in the GIF; the generous
# sleep just waits for a cold build to finish.
Hide
Type "go build -o /tmp/preflight-sim ./tools/sim" Enter
Sleep 6s
Type "export PREFLIGHT_SIM_BIN=/tmp/preflight-sim" Enter
Sleep 300ms
Type "source ./docs/assets/sim-preflight.sh" Enter
Sleep 300ms
Type "clear" Enter
Sleep 300ms
Show

Type "preflight apply playbooks/fleet-rollout.yml" Sleep 400ms Enter

# The active animation runs ~12s with streamed logs across four hosts, then
# the Recap summary holds. vhs ends the recording here, trimming the TUI's
# post-run linger.
Sleep 14s
