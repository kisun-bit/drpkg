//go:build !linux && !windows

package backend

import "fmt"

// cifsOnline 在未实现挂载逻辑的平台上返回不支持错误。
func cifsOnline(a *cifsAccessor) error {
	a.status = StatusError
	return fmt.Errorf("backend: network storage mount not supported on this platform (cifs)")
}

// cifsOffline 在未实现挂载逻辑的平台上仅重置为离线状态。
func cifsOffline(a *cifsAccessor) error {
	resetNetFs(&a.netFs)
	return nil
}

// nfsOnline 在未实现挂载逻辑的平台上返回不支持错误。
func nfsOnline(a *nfsAccessor) error {
	a.status = StatusError
	return fmt.Errorf("backend: network storage mount not supported on this platform (nfs)")
}

// nfsOffline 在未实现挂载逻辑的平台上仅重置为离线状态。
func nfsOffline(a *nfsAccessor) error {
	resetNetFs(&a.netFs)
	return nil
}

// resetNetFs 重置网络文件系统访问器的挂载状态字段。
func resetNetFs(n *netFs) {
	n.mounted = false
	n.mountDir = ""
	n.root = ""
	n.status = StatusOffline
}
