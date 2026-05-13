# Schedule A Windows Reboot

Use this guide when a managed Windows endpoint should reboot on a regular schedule. The `preflight/windows-power` action manages power plans and screen saver settings; scheduled reboots are regular tasks, so define them with the `scheduled_task` module.

## Add A Daily Reboot Task

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

`/r` reboots, `/f` closes running applications, and `/t 30` gives Windows a 30-second delay before restarting.

## Change The Time

Set `start_at` to the local target time when the reboot should run:

```yaml
      trigger: daily
      start_at: "04:30"
```

## Remove The Reboot Task

Use the same task identity with `ensure: absent`:

```yaml
tasks:
  - name: Remove daily reboot
    scheduled_task:
      path: Preflight
      name: Daily Reboot
      ensure: absent
```

## Combine With Power Settings

Keep power management and reboot scheduling as separate tasks:

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
