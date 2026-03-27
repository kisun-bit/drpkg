package ids

type Vendor struct {
	ID      uint16
	Name    string
	Devices []Device
}

type Device struct {
	ID   uint16
	Name string
}

type BaseClass struct {
	ID   uint16
	Name string
}
