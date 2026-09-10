package klog

import "golang.org/x/sys/windows"

// 保存已经打开的命名事件句柄
var eventHandle windows.Handle = windows.InvalidHandle
