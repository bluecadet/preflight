# Schedule A Windows Reboot

Use this guide when a managed Windows target should reboot on a regular
schedule — a common baseline for kiosks and exhibit PCs that run
continuously. Scheduled reboots are regular Windows scheduled tasks, so
define them with the `scheduled_task` module.

## Prerequisites

- A working playbook and target connection (see
  [Run a playbook](./run-a-playbook.md))
- A transport account allowed to create scheduled tasks that run as
  `SYSTEM`

## 1. Add A Daily Reboot Task

Create a playbook task that runs `shutdown.exe` from `C:\Windows\System32`:

```yaml
name: schedule-reboot

tasks:
  - name: Schedule daily reboot
    scheduled_task:
      path: Preflight
      name: Daily Reboot
      execute: C:\Windows\System32\shutdown.exe
      arguments: /r /f /t 30
      working_dir: C:\Windows\System32
      trigger: daily
      start_at: "03:00"
      run_as: SYSTEM
      run_level: highest
      enabled: true
      ensure: present
```

`/r` reboots, `/f` closes running applications, and `/t 30` gives Windows
a 30-second delay before restarting.

## 2. Set The Reboot Time

Set `start_at` to the local target time when the reboot should run:

```yaml
      trigger: daily
      start_at: "04:30"
```

## 3. Combine With Power Settings (Optional)

Reboot scheduling usually travels with a power baseline. Keep power
management and reboot scheduling as separate tasks:

```yaml
tasks:
  - name: Configure power baseline
    uses: preflight/windows-power
    with:
      plan_name: Exhibit Plan
      display_timeout_ac: 0
      sleep_timeout_ac: 0
      disable_screensaver: true

  - name: Schedule daily reboot
    scheduled_task:
      path: Preflight
      name: Daily Reboot
      execute: C:\Windows\System32\shutdown.exe
      arguments: /r /f /t 30
      working_dir: C:\Windows\System32
      trigger: daily
      start_at: "03:00"
      run_as: SYSTEM
      run_level: highest
      enabled: true
      ensure: present
```

## Remove The Reboot Task

Use the same task identity (`path` and `name`) with `ensure: absent`:

```yaml
tasks:
  - name: Remove daily reboot
    scheduled_task:
      path: Preflight
      name: Daily Reboot
      ensure: absent
```

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| The task exists but the machine never reboots | The task is disabled, or `start_at` is a time when the machine is powered off — check Task Scheduler under the `Preflight` folder on the target |
| Re-applying creates a second task instead of updating the first | The task identity changed — `path` and `name` together identify the task, so keep both stable across runs |

## Related Docs

- [Built-in module reference](../reference/modules.md) — `scheduled_task`
  fields
- [Embedded stdlib action reference](../reference/stdlib-actions.md) —
  `preflight/windows-power`
- [Run a playbook](./run-a-playbook.md)
