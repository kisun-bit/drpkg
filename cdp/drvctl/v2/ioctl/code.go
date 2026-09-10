package ioctl

// 驱动控制码
const (

	//
	// 任务管理
	//

	IOCTL_BIOTRK_START_TASK            = iota + 1 // 启动CDP任务
	IOCTL_BIOTRK_RELEASE_TASK                     // 释放并删除任务
	IOCTL_BIOTRK_GET_TASK_STATUS                  // 查询任务状态
	IOCTL_BIOTRK_SET_TASK_CONSISTENCY             // 设置任务一致性标记，转为CDP模式（实时模式）
	IOCTL_BIOTRK_GET_TASK_ERROR_STRING            // 获取错误字符串

	//
	// 受保护设备管理
	//

	IOCTL_BIOTRK_LIST_PROTECTED_DEVICE   // 列举已受保护的磁盘
	IOCTL_BIOTRK_ADD_PROTECTED_DEVICE    // 增加部分保护磁盘
	IOCTL_BIOTRK_REMOVE_PROTECTED_DEVICE // 移除部分保护磁盘

	//
	// 位图管理
	//

	IOCTL_BIOTRK_GET_BITMAP_DETAIL      // 获取位图详情
	IOCTL_BIOTRK_GET_BITMAP_DATA        // 获取位图数据
	IOCTL_BIOTRK_CLEAR_BITMAP_REFERENCE // 清理位图及引用

	//
	// 共享内存管理
	//

	IOCTL_BIOTRK_CREATE_SHM // 创建共享内存
	IOCTL_BIOTRK_DELETE_SHM // 删除共享内存，删除后若存在CDP任务，则自动转为CBT模式（位图模式）

	//
	// 日志管理
	//

	IOCTL_BIOTRK_SET_LOG_EVENT // 设置日志事件
	IOCTL_BIOTRK_GET_LOG       // 获取日志
)
