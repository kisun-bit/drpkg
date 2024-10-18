## 操作`QCow2`镜像文件的公共库

此公共库依赖于`qemu-img`和`qemu-iow`两个工具, 请于`github`的`libvirtdiskrw`仓库中(路径:`qemu-7.2.0/method2_executable/build/linux/amd64`)下载使用.
对于`qemu-img`, 如果你的环境中安装了`qemuutils`包, `qemu-img`会自动安装的, 但是`qemu-iow`是我们内部编写的, 必须从`libvirtdiskrw`仓库中下载

## 示例：
* [创建镜像](#创建镜像)
* [创建覆盖镜像](#创建覆盖镜像)
* [查询镜像信息](#查询镜像信息)
* [查询镜像信息(附带所有后备文件)](#查询镜像信息附带所有后备文件)
* [提交](#提交)
* [变基](#变基)
* [删除](#删除)
* [如何进行一次快照链合并](#如何进行一次快照链合并操作)
* [只写(只有写, 不存在读, 一般用于介质的场景)](#只写只有写-不存在读-一般用于介质的场景)
* [可读可写(既支持读, 也支持写, 一般用于`fuse`内容寻址的场景)](#可读可写既支持读-也支持写-一般用于fuse内容寻址的场景)
* [有效数据读取](#有效数据读取)

### 创建镜像
```go
// 示例：
// 创建一个大小为1TB且路径为/path/to/demo.qcow2的镜像文件.
CreateQCow2("/path/to/demo.qcow2", 1<<40)
```

### 创建覆盖镜像
```go
// 示例：
// 基于/path/to/base.qcow2创建一个大小为1TB且路径为/path/to/overlay.qcow2的覆盖镜像文件.
// 这样一来, /path/to/base.qcow2就变成了/path/to/overlay.qcow2的后备文件.
// 创建成功后会形成一个链式结构: [/path/to/base.qcow2] <-- [/path/to/overlay.qcow2]
CreateOverlayQCow2("/path/to/overlay.qcow2", "/path/to/base.qcow2", 1<<40)
```

### 查询镜像信息
```go
// 示例:
GeneralInfoQCow2("/path/to/demo.qcow2")
```

### 查询镜像信息(附带所有后备文件)
```go
// 示例:
GeneralInfoQCow2BackingList("/path/to/demo.qcow2")
```

### 提交
```go
// 示例：
// 假设存在[/path/to/base.qcow2] <-- [/path/to/overlay.qcow2]这样的链式结构,
// 现在我需要将/path/to/overlay.qcow2的数据提交(合并)到/path/to/base.qcow2中, 那么:
CommitQCow2("/path/to/overlay.qcow2")
```

### 变基
```go
// 示例：
// 假设存在[1.qcow2] <-- [2.qcow2] <- [3.qcow2]这样的链式结构, 现在我想将3.qcow2的
// 基础镜像(/后备文件)由2.qcow2变更为1.qcow2, 那么：
RebaseQCow2("3.qcow2", "1.qcow2")
```

### 删除
```go
// 示例
RemoveQCow2("/path/to/demo.qcow2")
```

### 如何进行一次快照链合并操作？
```
假设存在1.qcow2 <-- 2.qcow2 <-- 3.qcow这样的快照链结构.
此时我想将快照链上的节点格式由3个变成2个, 那么需执行下述步骤：
1. 对2.qcow2执行【提交】：这一步实现2.qcow2的数据全部合并至1.qcow2
2. 对3.qcow2执行【变基】：这一步将3.qcow2的基础镜像(/后备文件)由2.qcow2变更为1.qcow2
3. 将2.qcow2【删除】
通过上述三步, 快照链就变成了1.qcow2 <-- 3.qcow2
```

### 只写(只有写, 不存在读, 一般用于介质的场景)
详细的使用参考: `examples/copy_device_to_image_single/main.go`
```go
// 初始化环境,若提前将qemu-img和qemu-iow放入了环境变量中,可忽略此步.
QemuEnvSetup("/path/to/qemu-img", "/path/to/qemu-iow")

// 初始化一个IO管理器. 
// 这种场景下请注意：
// 1. 不用带上EnableRWSerialAccess选项, 这个会严重拉低性能.
// 2. WritebackWithAio、DirectWithAio的性能高于Writeback、Direct.
NewQemuIOManager("/path/to/demo.qcow2", "qcow2", WritebackWithAio)

// 初始话完毕后, 此IO管理器就实现了Open、ReadAt、WriteAt方法
// do something...
```

### 可读可写(既支持读, 也支持写, 一般用于`fuse`内容寻址的场景)
详细的使用参考: `examples/read_and_write/main.go`
```go
// 初始化环境,若提前将qemu-img和qemu-iow放入了环境变量中,可忽略此步.
QemuEnvSetup("/path/to/qemu-img", "/path/to/qemu-iow")

// 初始化一个IO管理器. 
// 这种场景下请注意:
// 1. 一定要带上EnableRWSerialAccess选项.
// 2. IO模式的参数只能是Writeback、Direct之一.
NewQemuIOManager("/path/to/demo.qcow2", "qcow2", Writeback, EnableRWSerialAccess())

// 初始话完毕后, 此IO管理器就实现了Open、ReadAt、WriteAt方法
// do something...
```

### 有效数据读取
详细的使用参考: `examples/read_effect_blocks/main.go`