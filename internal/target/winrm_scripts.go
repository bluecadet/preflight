package target

import "github.com/bluecadet/preflight/internal/pscript"

const serviceCheckScript = pscript.ServiceCheckScript

const serviceApplyScript = pscript.ServiceApplyScript

const packageCheckScript = pscript.PackageCheckScript

const packageApplyScript = pscript.PackageApplyScript

const shortcutCheckScript = pscript.ShortcutCheckScript

const shortcutApplyScript = pscript.ShortcutApplyScript

const scheduledTaskCheckScript = pscript.ScheduledTaskCheckScript

const scheduledTaskApplyScript = pscript.ScheduledTaskApplyScript

const wingetPackageCheckScript = pscript.WingetPackageCheckScript

const wingetPackageApplyScript = pscript.WingetPackageApplyScript

const userCheckScript = pscript.UserCheckScript

const userApplyScript = pscript.UserApplyScript

const firewallRuleCheckScript = pscript.FirewallRuleCheckScript

const firewallRuleApplyScript = pscript.FirewallRuleApplyScript

const registryCheckScript = pscript.RegistryCheckScript

const registryApplyScript = pscript.RegistryApplyScript

// registryEnsureScript combines check and apply in one PowerShell invocation.
// It outputs "ok", "changed", or "would-change" (dry-run). $__pf_dry_run must
// be injected by the caller before $params.
const registryEnsureScript = pscript.RegistryEnsureScript

const removeAppxPackagesCheckScript = pscript.RemoveAppxCheckScriptWithOutput

const removeAppxPackagesApplyScript = pscript.RemoveAppxApplyScript

// removeAppxPackagesEnsureScript combines check and apply in one invocation,
// calling Get-AppxProvisionedPackage -Online exactly once regardless of outcome.
// Outputs "ok", "would-change" (dry-run), or "changed". $__pf_dry_run must be
// set before $params by the caller.
const removeAppxPackagesEnsureScript = pscript.RemoveAppxEnsureScript

const powerPlanCheckScript = pscript.PowerPlanCheckScript

const powerPlanApplyScript = pscript.PowerPlanApplyScript

const windowsFeatureCheckScript = pscript.WindowsFeatureCheckScript

const windowsFeatureApplyScript = pscript.WindowsFeatureApplyScript
