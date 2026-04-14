package recovery

import "os"

func vmbusExisted() (bool, error) {
	items, err := os.ReadDir("/sys/bus/vmbus/devices")
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return len(items) > 0, nil
}
