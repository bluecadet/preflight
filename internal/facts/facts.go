package facts

// OSFacts holds operating system information about a target.
type OSFacts struct {
	Name           string // os-release ID (POSIX) or friendly name (Windows)
	Version        string // os-release VERSION_ID (POSIX) or Windows version
	Build          int    // Windows build number; 0 on POSIX (Windows-only)
	Arch           string
	Hostname       string
	Family         string // windows | linux | darwin | unknown
	PackageManager string // apt | dnf | "" (POSIX only)
	Init           string // systemd | "" (POSIX only)
}

// DiskFacts holds disk space information for a single drive.
type DiskFacts struct {
	Path    string // e.g. "C:"
	TotalGB float64
	FreeGB  float64
	UsedGB  float64
}

// Facts is the complete set of facts gathered from a target.
type Facts struct {
	OS       OSFacts
	Disks    []DiskFacts
	Env      map[string]string // target environment variables
	Hostname string
}

// AsMap converts Facts to a nested map[string]any for use in templates.
// The facts.os map always contains every key: family, name, version,
// package_manager, and init are present even when a signal is absent, in
// which case they render as empty strings (build is 0). This lets playbooks
// branch on {{ facts.os.family }} etc. without distinguishing missing from
// empty.
//
// Keys: facts.os.family, facts.os.name, facts.os.version, facts.os.build,
//
//	facts.os.arch, facts.os.hostname, facts.os.package_manager, facts.os.init,
//	facts.hostname, facts.disks (list), facts.env.*
func (f *Facts) AsMap() map[string]any {
	osMap := map[string]any{
		"name":            f.OS.Name,
		"version":         f.OS.Version,
		"build":           f.OS.Build,
		"arch":            f.OS.Arch,
		"hostname":        f.OS.Hostname,
		"family":          f.OS.Family,
		"package_manager": f.OS.PackageManager,
		"init":            f.OS.Init,
	}

	disks := make([]map[string]any, len(f.Disks))
	for i, d := range f.Disks {
		disks[i] = map[string]any{
			"path":     d.Path,
			"total_gb": d.TotalGB,
			"free_gb":  d.FreeGB,
			"used_gb":  d.UsedGB,
		}
	}

	env := make(map[string]any, len(f.Env))
	for k, v := range f.Env {
		env[k] = v
	}

	return map[string]any{
		"os":       osMap,
		"disks":    disks,
		"env":      env,
		"hostname": f.Hostname,
	}
}
