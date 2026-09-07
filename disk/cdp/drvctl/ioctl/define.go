package ioctl

const MaxnameLen int = 256

const MsgBuffersize = 512

const DiskSpacePerBit = 4 << 20

// 定义 ioctl 常量（等价 C 中的 _IO(BIOTRK_IOCTL_MAGIC, 0) 等）
const (
	BIOTRK_CMD_NOP               = 0
	BIOTRK_CMD_START             = 1  // 启动
	BIOTRK_CMD_ADD               = 2  // 增加部分保护磁盘
	BIOTRK_CMD_REDUCE            = 3  // 移除部分保护磁盘
	BIOTRK_CMD_DELETE            = 4  // 删除任务
	BIOTRK_CMD_SUSPEND           = 5  // 切换到定时模式
	BIOTRK_CMD_CONSISTENT        = 6  // 设置一致性标记，切换到实时模式
	BIOTRK_CMD_STATUS            = 7  // 状态查询
	BIOTRK_CMD_CLEAR_BITMAP      = 8  // 清理位图
	BIOTRK_CMD_SELECT_BITMAP     = 9  // 查询位图大小
	BIOTRK_CMD_GET_BITMAP        = 10 // 获取位图内容
	BIOTRK_CMD_CREATE_RINGBUFFER = 11 // 创建ringbuffer
	BIOTRK_CMD_DELETE_RINGBUFFER = 12 // 删除ringbuffer
	BIOTRK_CMD_SET_LOG_EVENT     = 13 // 设置日志事件
	BIOTRK_CMD_GET_LOG           = 14 // 获取日志
	BIOTRK_CMD_GET_ERR_STR       = 15 // 获取错误字符串

	BIOTRK_CMD_SELECT_ALL_BITS = 50 // 测试 打印所有位图
	BIOTRK_CMD_SELECT_ALL_MEM  = 51 // 测试 打印驱动内部k/vmalloc
)
