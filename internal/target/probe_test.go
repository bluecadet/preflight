package target

import (
	"strings"
	"testing"
)

func TestParsePOSIXProbe_Full(t *testing.T) {
	out := strings.Join([]string{
		"hostname=kiosk-a",
		"kernel=Linux",
		"arch=x86_64",
		"os_name=ubuntu",
		"os_version=22.04",
		"package_manager=apt",
		"init=systemd",
		"euid=1000",
		"sudo=1",
		"",
	}, "\n")
	p := parsePOSIXProbe(out)
	if p.Hostname != "kiosk-a" {
		t.Errorf("hostname: got %q", p.Hostname)
	}
	if p.Kernel != "Linux" {
		t.Errorf("kernel: got %q", p.Kernel)
	}
	if p.Arch != "x86_64" {
		t.Errorf("arch: got %q", p.Arch)
	}
	if p.OSName != "ubuntu" {
		t.Errorf("os_name: got %q", p.OSName)
	}
	if p.OSVersion != "22.04" {
		t.Errorf("os_version: got %q", p.OSVersion)
	}
	if p.PackageManager != "apt" {
		t.Errorf("package_manager: got %q", p.PackageManager)
	}
	if p.Init != "systemd" {
		t.Errorf("init: got %q", p.Init)
	}
	if p.EffectiveUID != "1000" {
		t.Errorf("euid: got %q", p.EffectiveUID)
	}
	if !p.SudoAvailable {
		t.Error("sudo: expected true")
	}
}

func TestParsePOSIXProbe_AbsentSignalsAreEmpty(t *testing.T) {
	// macOS has no os-release, no apt/dnf, no systemd. Every absent signal
	// must be an empty string, never a missing field, and the probe must
	// not fail. euid/sudo default to empty/false when absent.
	out := strings.Join([]string{
		"hostname=mbp",
		"kernel=Darwin",
		"arch=arm64",
		"os_name=",
		"os_version=",
		"package_manager=",
		"init=",
		"",
	}, "\n")
	p := parsePOSIXProbe(out)
	if p.OSName != "" || p.OSVersion != "" || p.PackageManager != "" || p.Init != "" {
		t.Fatalf("expected all absent signals empty, got %+v", p)
	}
	if p.EffectiveUID != "" {
		t.Fatalf("expected empty euid, got %q", p.EffectiveUID)
	}
	if p.SudoAvailable {
		t.Fatal("expected sudo false when absent")
	}
	if p.Hostname != "mbp" || p.Kernel != "Darwin" || p.Arch != "arm64" {
		t.Fatalf("present signals should still parse, got %+v", p)
	}
}

func TestParsePOSIXProbe_PartialAndOutOfOrder(t *testing.T) {
	// A truncated or reordered payload must not error; known keys are read,
	// unknown keys are ignored, missing keys default to empty.
	out := "init=systemd\nhostname=kiosk-b\nbogus=ignored\narch=aarch64\n"
	p := parsePOSIXProbe(out)
	if p.Hostname != "kiosk-b" {
		t.Errorf("hostname: got %q", p.Hostname)
	}
	if p.Arch != "aarch64" {
		t.Errorf("arch: got %q", p.Arch)
	}
	if p.Init != "systemd" {
		t.Errorf("init: got %q", p.Init)
	}
	if p.Kernel != "" || p.OSName != "" || p.OSVersion != "" || p.PackageManager != "" {
		t.Fatalf("expected missing signals empty, got %+v", p)
	}
}

func TestParsePOSIXProbe_EmptyOutput(t *testing.T) {
	// A completely empty payload (e.g. transport returned nothing) must not
	// panic or error; every field is empty.
	p := parsePOSIXProbe("")
	if p != (Probe{}) {
		t.Fatalf("expected zero-value probe, got %+v", p)
	}
}

func TestParsePOSIXProbe_DnfPackageManager(t *testing.T) {
	out := strings.Join([]string{
		"hostname=rocky-host",
		"kernel=Linux",
		"arch=x86_64",
		"os_name=rocky",
		"os_version=9.3",
		"package_manager=dnf",
		"init=systemd",
		"euid=0",
		"sudo=0",
		"",
	}, "\n")
	p := parsePOSIXProbe(out)
	if p.PackageManager != "dnf" {
		t.Errorf("expected dnf, got %q", p.PackageManager)
	}
	if p.OSName != "rocky" {
		t.Errorf("os_name: got %q", p.OSName)
	}
	if p.EffectiveUID != "0" {
		t.Errorf("euid: got %q", p.EffectiveUID)
	}
	if p.SudoAvailable {
		t.Error("expected sudo false")
	}
}
