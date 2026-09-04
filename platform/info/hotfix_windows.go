package info

import (
	wmi_ "github.com/yusufpapurcu/wmi"
)

func QueryHotfixList() ([]Hotfix, error) {
	var updates []Hotfix
	query := "SELECT HotFixID, Description, InstalledOn, InstalledBy FROM Win32_QuickFixEngineering"

	err := wmi_.Query(query, &updates)
	if err != nil {
		return nil, err
	}

	return updates, err
}
