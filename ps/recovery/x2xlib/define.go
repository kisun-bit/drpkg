package x2xlib

import (
	"fmt"

	"github.com/kisun-bit/drpkg/define"
)

const (
	driverTypeNormal uint16 = iota
	driverTypeVirtualKvm
	driverTypeVirtualXen
	driverTypeVirtualVmware
	driverTypeVirtualHyperV
)

var SupportedOsTypes = []string{
	define.OsWindows,
	define.OsLinux,
}

var SupportedArchTypes = []string{
	define.ArchAmd64,
	define.ArchArm64,
	define.Arch386,
	define.ArchLoong64,
	define.ArchRiscv64,
}

//var SupportedDistroTypes = []string{
//	// Microsoft
//	define.DistroMicrosoft,
//
//	// RHEL family
//	define.DistroFedora,
//	define.DistroRHEL,
//	define.DistroCentOS,
//	define.DistroCircle,
//	define.DistroScientificLinux,
//	define.DistroRedhatBased,
//	define.DistroOracleLinux,
//	define.DistroRocky,
//	define.DistroKylin,
//	define.DistroNeoKylin,
//	define.DistroAnolis,
//	define.DistroOpenEuler,
//	define.DistroAlma,
//
//	// ALT family
//	define.DistroALTLinux,
//
//	// SUSE family
//	define.DistroSLES,
//	define.DistroSUSEBased,
//	define.DistroOpenSUSE,
//
//	// Debian family
//	define.DistroDebian,
//	define.DistroUbuntu,
//	define.DistroLinuxMint,
//	define.DistroKaliLinux,
//}

var SupportedFamilyTypes = []string{
	define.LinuxFamilyRHEL,
	define.LinuxFamilyRHEL,
	define.LinuxFamilySUSE,
	define.WindowsFamily,
}

var SupportedVirtualizationTypes = []define.HPVirtType{
	define.HPVTVmware,
	define.HPVTXen,
	define.HPVTKvm,
	define.HPVTHyperV,
}

var SupportedBusTypes = []uint32{
	//0x00, //"Unclassified device",
	0x01, //"Mass storage controller",
	0x02, //"Network controller",
	0x03, //"Display controller",
	0x04, //"Multimedia controller",
	0x05, //"Memory controller",
	0x06, //"Bridge",
	0x07, //"Communication controller",
	0x08, //"Generic system peripheral",
	0x09, //"Input device controller",
	0x0A, //"Docking station",
	0x0B, //"Processor",
	0x0C, //"Serial bus controller",
	0x0D, //"Wireless controller",
	0x0E, //"Intelligent controller",
	0x0F, //"Satellite communications controller",
	0x10, //"Encryption controller",
	0x11, //"Signal processing controller",
	0x12, //"Processing accelerators",
	0x13, //"Non-Essential Instrumentation",
	0x40, //"Coprocessor",
	//0xFF, //"Unassigned class",
}

var SupportedDriverExt = []string{
	"sys",
	"inf",
	"cat",
	"pdb",
	"deb",
	"rpm",
}

const driverStoreDirName = "driverstore.H0nK1"

var driverStoreDBName = fmt.Sprintf("%s.db", driverStoreDirName)
