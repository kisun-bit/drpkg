package info

type LinuxTarget struct {
	GoArch     string `json:"goArch"`     // Architecture name according to Go
	LinuxArch  string `json:"linuxArch"`  // Architecture name according to the Linux Kernel
	GNUArch    string `json:"GNUArch"`    // Architecture name according to GNU tools (https://wiki.debian.org/Multiarch/Tuples)
	BigEndian  bool   `json:"bigEndian"`  // Default Little Endian
	SignedChar bool   `json:"signedChar"` // Is -fsigned-char needed (default no)
	Bits       int    `json:"bits"`
}
