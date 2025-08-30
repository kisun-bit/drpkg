package info

import "runtime"

type LinuxTarget struct {
	GoArch     string `json:"goArch"`     // Architecture name according to Go
	LinuxArch  string `json:"linuxArch"`  // Architecture name according to the Linux Kernel
	GNUArch    string `json:"GNUArch"`    // Architecture name according to GNU tools (https://wiki.debian.org/Multiarch/Tuples)
	BigEndian  bool   `json:"bigEndian"`  // Default Little Endian
	SignedChar bool   `json:"signedChar"` // Is -fsigned-char needed (default no)
	Bits       int    `json:"bits"`
}

var LinuxTargets = []LinuxTarget{
	{
		GoArch:    "386",
		LinuxArch: "x86",
		GNUArch:   "i686-linux-gnu", // Note "i686" not "i386"
		// 举例:
		// [root@centos6 ~]# uname --help
		// Usage: uname [OPTION]...
		// Print certain system information.  With no OPTION, same as -s.
		//
		//   -a, --all                print all information, in the following order,
		//                              except omit -p and -i if unknown:
		//   -s, --kernel-name        print the kernel name
		//   -n, --nodename           print the network node hostname
		//   -r, --kernel-release     print the kernel release
		//   -v, --kernel-version     print the kernel version
		//   -m, --machine            print the machine hardware name
		//   -p, --processor          print the processor type or "unknown"
		//   -i, --hardware-platform  print the hardware platform or "unknown"
		//   -o, --operating-system   print the operating system
		//       --help     display this help and exit
		//       --version  output version information and exit
		//
		// [root@centos6 ~]#
		// [root@centos6 ~]# uname --a
		// Linux centos6 2.6.32-696.el6.i686 #1 SMP Tue Mar 21 18:53:30 UTC 2017 i686 i686 i386 GNU/Linux
		// [root@centos6 ~]#
		// [root@centos6 ~]# uname -i
		// i386
		// [root@centos6 ~]# uname -p
		// i686
		// [root@centos6 ~]# uname -m
		// i686
		Bits: 32,
	},
	{
		GoArch:    "amd64",
		LinuxArch: "x86",
		GNUArch:   "x86_64-linux-gnu",
		Bits:      64,
	},
	{
		GoArch:     "arm64",
		LinuxArch:  "arm64",
		GNUArch:    "aarch64-linux-gnu",
		SignedChar: true,
		Bits:       64,
	},
	{
		GoArch:    "arm",
		LinuxArch: "arm",
		GNUArch:   "arm-linux-gnueabi",
		Bits:      32,
	},
	{
		GoArch:    "loong64",
		LinuxArch: "loongarch",
		GNUArch:   "loongarch64-linux-gnu",
		Bits:      64,
	},
	{
		GoArch:    "mips",
		LinuxArch: "mips",
		GNUArch:   "mips-linux-gnu",
		BigEndian: true,
		Bits:      32,
	},
	{
		GoArch:    "mipsle",
		LinuxArch: "mips",
		GNUArch:   "mipsel-linux-gnu",
		Bits:      32,
	},
	{
		GoArch:    "mips64",
		LinuxArch: "mips",
		GNUArch:   "mips64-linux-gnuabi64",
		BigEndian: true,
		Bits:      64,
	},
	{
		GoArch:    "mips64le",
		LinuxArch: "mips",
		GNUArch:   "mips64el-linux-gnuabi64",
		Bits:      64,
	},
	{
		GoArch:    "ppc",
		LinuxArch: "powerpc",
		GNUArch:   "powerpc-linux-gnu",
		BigEndian: true,
		Bits:      32,
	},
	{
		GoArch:    "ppc64",
		LinuxArch: "powerpc",
		GNUArch:   "powerpc64-linux-gnu",
		BigEndian: true,
		Bits:      64,
	},
	{
		GoArch:    "ppc64le",
		LinuxArch: "powerpc",
		GNUArch:   "powerpc64le-linux-gnu",
		Bits:      64,
	},
	{
		GoArch:    "riscv64",
		LinuxArch: "riscv",
		GNUArch:   "riscv64-linux-gnu",
		Bits:      64,
	},
	{
		GoArch:     "s390x",
		LinuxArch:  "s390",
		GNUArch:    "s390x-linux-gnu",
		BigEndian:  true,
		SignedChar: true,
		Bits:       64,
	},
	{
		GoArch:    "sparc64",
		LinuxArch: "sparc",
		GNUArch:   "sparc64-linux-gnu",
		BigEndian: true,
		Bits:      64,
	},
}

func QueryLinuxTarget() LinuxTarget {
	goArch := runtime.GOARCH
	for _, lt := range LinuxTargets {
		if lt.GoArch == goArch {
			return lt
		}
	}
	// 未知架构
	return LinuxTarget{
		GoArch: goArch,
		Bits:   0,
	}
}
