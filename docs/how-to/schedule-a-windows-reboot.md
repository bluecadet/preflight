# Schedule a Windows reboot

Reboot a Windows target on a daily schedule with a `scheduled_task` that runs `shutdown.exe`.

## Prerequisites

- A playbook and a working target connection ([Run a playbook](./run-a-playbook.md))
- A transport account that can create scheduled tasks running as `SYSTEM`, such as a local administrator

## 1. Add the reboot task

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
```

`/r` restarts, `/f` closes running applications, and `/t 30` waits 30 seconds first. `start_at` is the target's local time.

## 2. Apply and verify

```bash
preflight apply playbooks/schedule-reboot.yml
```

On the target, Task Scheduler shows `Daily Reboot` in the `Preflight` folder.

## Pair with a power baseline (optional)

Add a separate task for power settings so the machine stays awake between reboots:

```yaml
  - name: Configure power baseline
    uses: preflight/windows-power
    with:
      plan_name: Exhibit Plan
      display_timeout_ac: 0
      sleep_timeout_ac: 0
      disable_screensaver: true
```

## Remove the reboot task

Keep `path` and `name` and set `ensure: absent`:

```yaml
tasks:
  - name: Remove daily reboot
    scheduled_task:
      path: Preflight
      name: Daily Reboot
      ensure: absent
```

## Troubleshooting

| Symptom | Cause |
|---------|-------|
| The task exists but the machine never reboots | The task is disabled, or the machine is off at `start_at`. Check the task's history in Task Scheduler. |
| A second task appears instead of an update | `path` or `name` changed. Together they identify the task. |

## Related docs

- [Built-in module reference](../reference/modules.md)
- [Stdlib actions reference](../reference/stdlib-actions.md)
