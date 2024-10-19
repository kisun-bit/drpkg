package efi

type EfiVariable struct {
	Namespace string
	Name      string
	Value     []byte
}
