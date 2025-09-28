package pci

// VPci 虚拟PCI
// 用于通用表示各系统的PCI硬件
type VPci struct {
	JsonDetails string `json:"jsonDetails"`
	ModelName   string `json:"modelName"`
	HardwareID  string `json:"hardwareID"`
}
