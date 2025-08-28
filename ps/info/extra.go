package info

type LinuxKernel struct {
	Name      string
	Vmlinuz   string
	SystemMap string
	Config    string
	Initrd    string
	Bootable  bool
	Default   bool
}

type LinuxRelease struct {
	Distro    string
	ReleaseID string
	Version   string
}
