//go:build linux

package efi

import (
	"fmt"
	"os"
	"strings"
)

//
// 请参考：https://github.com/Foxboron/go-uefi
//

const efiPath = "/sys/firmware/efi/efivars/"

func GetEfiVariables() ([]EfiVariable, error) {
	var result []EfiVariable
	files, err := os.ReadDir(efiPath)
	if err != nil {
		return nil, err
	}

	for _, file := range files {
		filename := file.Name()
		parts := strings.SplitN(filename, "-", 2)
		if len(parts) != 2 {
			continue
		}
		result = append(result, EfiVariable{
			Namespace: fmt.Sprintf("{%s}", parts[1]),
			Name:      parts[0],
			Value:     nil,
		})
	}
	return result, nil
}

func GetEfiVariableValue(namespace string, name string) ([]byte, error) {
	namespace = strings.Trim(namespace, "{}")

	return os.ReadFile(fmt.Sprintf("%s%s-%s", efiPath, name, namespace))
}
