package facts

// OSFacts holds operating system information about a target.
type OSFacts struct {
	Name     string // e.g. "Windows 10"
	Version  string // e.g. "10.0.19041"
	Build    int    // e.g. 19041
	Arch     string // e.g. "amd64"
	Hostname string
}

// DiskFacts holds disk space information for a single drive.
type DiskFacts struct {
	Path    string  // e.g. "C:"
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

// AsMap converts Facts to a nested map[string]interface{} for use in templates.
// Keys: facts.os.name, facts.os.build, facts.os.version, facts.os.arch,
//
//	facts.hostname, facts.disks (list), facts.env.*
func (f *Facts) AsMap() map[string]interface{} {
	osMap := map[string]interface{}{
		"name":     f.OS.Name,
		"version":  f.OS.Version,
		"build":    f.OS.Build,
		"arch":     f.OS.Arch,
		"hostname": f.OS.Hostname,
	}

	disks := make([]map[string]interface{}, len(f.Disks))
	for i, d := range f.Disks {
		disks[i] = map[string]interface{}{
			"path":    d.Path,
			"total_gb": d.TotalGB,
			"free_gb":  d.FreeGB,
			"used_gb":  d.UsedGB,
		}
	}

	env := make(map[string]interface{}, len(f.Env))
	for k, v := range f.Env {
		env[k] = v
	}

	return map[string]interface{}{
		"os":       osMap,
		"disks":    disks,
		"env":      env,
		"hostname": f.Hostname,
	}
}
