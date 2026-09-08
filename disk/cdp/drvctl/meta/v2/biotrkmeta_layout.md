# BIOTRKMETA 元数据文件格式规范 v1.0

## 1. 概述

BIOTRKMETA 是 CDP 系统用于保存受保护设备、磁盘区间以及 Bitmap 元数据的专用文件格式。

该文件用于描述：

* CDP 当前运行状态；
* 受保护设备及其设备标识；
* 受保护设备对应的物理磁盘区间；
* 磁盘区间与 Bitmap Unit 的映射关系；
* Bitmap Unit 的分配状态；
* Bitmap Unit 内部的 Dirty Bitmap 数据。

文件采用：

```text
固定大小 Header
    +
Protected Device Records
    +
Bitmap Unit Allocation Map
    +
Bitmap Unit Data
```

的组织方式。

每一个 `ProtectedDevice` 对应文件中的一条独立记录。

`ProtectedDevice` 记录内部直接保存设备类型、设备 ID 以及该设备对应的所有受保护磁盘区间，不再通过额外的 Device ID Mapping 或 VirtualID 进行间接引用。

---

# 2. 全局约定

| 项目                  | 说明                                           |
| :------------------ | :------------------------------------------- |
| **字节序**             | 小端序（Little-Endian）                           |
| **字符编码**            | UTF-8                                        |
| **长度单位**            | 所有长度字段单位均为 Byte                              |
| **偏移单位**            | 所有 Offset 字段单位均为 Byte                        |
| **Header 大小**       | 固定 4096 Byte                                 |
| **保留字段**            | 必须填充 `0x00`                                  |
| **Header 校验**       | Header 包含 CRC32 校验                           |
| **Header CRC32 计算** | 计算 Header CRC32 时，`HeaderCRC32` 字段本身按 `0` 处理 |
| **结构对齐**            | 文件结构字段按照格式定义的固定大小存储，不依赖编译器结构体对齐              |
| **DeviceID 最大长度**   | 512 Byte                                     |

---

# 3. 文件总体布局

文件由以下区域组成：

```text
+------------------------------------------+  Offset 0
│ 1. Header                                │
│    固定 4096 Byte                        │
+------------------------------------------+  Header.ProtectedRegionOffset
│ 2. Protected Device Records              │
│    受保护设备及磁盘区间描述              │
+------------------------------------------+  Header.BitmapAllocMapOffset
│ 3. Bitmap Unit Allocation Map            │
│    Bitmap Unit 分配状态                  │
+------------------------------------------+  Header.BitmapDataOffset
│ 4. Bitmap Unit Data                      │
│    Bitmap Unit 实际位图数据              │
+------------------------------------------+
```

文件区域关系：

```text
Header
    ↓
Protected Device Records
    ↓
Bitmap Unit Allocation Map
    ↓
Bitmap Unit Data
```

其中：

* Header 固定为 4096 Byte；
* Protected Device Records 由多个 `ProtectedDevice` 记录组成；
* Bitmap Unit Allocation Map 使用 Bit 表示 Bitmap Unit 是否已经分配；
* Bitmap Unit Data 保存已经分配的 Bitmap Unit 的 Dirty Bitmap。

---

# 4. Header 区域

## 4.1 Header 概述

Header 固定为：

```text
4096 Byte
```

文件 Offset：

```text
0 ~ 4095
```

均属于 Header 区域。

未使用字段及 Reserved 区域必须填充：

```text
0x00
```

---

## 4.2 Header 结构

| 偏移  |    大小 | 字段名                     | 说明                              |
| :-- | ----: | :---------------------- | :------------------------------ |
| 0   |   16B | `Signature`             | 文件签名 `"BIOTRKMETA"`             |
| 16  |    4B | `Version`               | 格式版本号                           |
| 20  |    4B | `CDPStatus`             | CDP 当前状态                        |
| 24  |    8B | `ErrorCode`             | CDP 错误码                         |
| 32  |    8B | `BitIndexSpace`         | 一个 Bitmap Bit 对应的磁盘空间大小         |
| 40  |    4B | `BitmapClusterSize`     | 一个 Bitmap Unit 的数据大小            |
| 44  |    4B | `TotalBitmapUnits`      | Bitmap Unit 总数量                 |
| 48  |    8B | `ProtectedRegionOffset` | Protected Device Records 起始偏移   |
| 56  |    8B | `ProtectedRegionSize`   | Protected Device Records 区域大小   |
| 64  |    4B | `ProtectedDeviceCount`  | ProtectedDevice 记录数量            |
| 68  |    4B | `WorkMode`              | 当前驱动工作模式                        |
| 72  |    8B | `BitmapAllocMapOffset`  | Bitmap Unit Allocation Map 起始偏移 |
| 80  |    8B | `BitmapAllocMapSize`    | Bitmap Unit Allocation Map 大小   |
| 88  |    8B | `BitmapDataOffset`      | Bitmap Unit Data 起始偏移           |
| 96  |    4B | `HeaderCRC32`           | Header CRC32 校验值                |
| 100 | 3996B | `Reserved`              | 保留区域，填充 `0x00`                  |

Header 总大小：

```text
4096 Byte
```

---

# 5. Header 字段说明

## 5.1 Signature

固定值：

```text
BIOTRKMETA
```

占用 16 Byte。

不足部分使用：

```text
0x00
```

填充。

例如：

```text
42 49 4F 54 52 4B 4D 45 54 41 00 00 00 00 00 00
```

---

## 5.2 Version

格式：

```text
31                         16 15                          0
+----------------------------+-----------------------------+
│       Major Version        │       Minor Version         │
+----------------------------+-----------------------------+
```

计算方式：

```text
Major = Version >> 16
Minor = Version & 0xFFFF
```

v1.0：

```text
Version = 0x00010000
```

即：

```text
Major = 1
Minor = 0
```

---

## 5.3 CDPStatus

表示 CDP 当前状态。

|   值 | 含义     |
| --: | :----- |
| `0` | 空闲     |
| `1` | 实时 CDP |
| `2` | 定时 CDP |
| `3` | 错误     |

其他值暂未定义。

---

## 5.4 ErrorCode

表示 CDP 错误状态。

|   值 | 含义                       |
| --: | :----------------------- |
| `0` | 正常                       |
| `1` | 上一次未正常关机                 |
|  其他 | 由 Windows / Linux 驱动自行定义 |

---

## 5.5 BitIndexSpace

一个 Bitmap Bit 对应的磁盘空间大小。

单位：

```text
Byte
```

默认：

```text
4194304 Byte
```

即：

```text
4 MiB
```

例如：

```text
BitIndexSpace = 4194304
```

表示第 `N` 个 Bitmap Bit 对应：

```text
[
    N × 4194304,
    (N + 1) × 4194304
)
```

范围内的磁盘空间。

---

## 5.6 BitmapClusterSize

一个 Bitmap Unit 实际占用的数据大小。

默认：

```text
4096 Byte
```

一个 Bitmap Unit 包含：

```text
4096 × 8
=
32768 Bit
```

---

## 5.7 TotalBitmapUnits

Bitmap Unit 总数量。

默认：

```text
8192
```

Bitmap Bit 总数量：

```text
TotalBitmapBits =
    TotalBitmapUnits
    × BitmapClusterSize
    × 8
```

默认：

```text
8192 × 4096 × 8
=
268,435,456 Bit
```

---

## 5.8 ProtectedRegionOffset

Protected Device Records 区域起始偏移。

单位：

```text
Byte
```

必须满足：

```text
ProtectedRegionOffset >= 4096
```

---

## 5.9 ProtectedRegionSize

Protected Device Records 区域总大小。

由于所有 `ProtectedDevice` 记录采用固定大小，因此：

```text
ProtectedRegionSize =
    ProtectedDeviceCount
    × ProtectedDeviceRecordSize
```

---

## 5.10 ProtectedDeviceCount

ProtectedDevice 记录数量。

每一条记录就是一个完整的：

```text
ProtectedDevice
```

记录。

---

## 5.11 WorkMode

当前驱动工作模式。

|   值 | 含义                  |
| --: | :------------------ |
| `0` | 普通模式                |
| `1` | Linux 启用预处理阶段拷贝数据模式 |

其他值暂未定义。

---

## 5.12 BitmapAllocMapOffset

Bitmap Unit Allocation Map 区域起始偏移。

---

## 5.13 BitmapAllocMapSize

Bitmap Unit Allocation Map 区域大小。

最小值：

```text
(TotalBitmapUnits + 7) / 8
```

---

## 5.14 BitmapDataOffset

Bitmap Unit Data 区域起始偏移。

---

## 5.15 HeaderCRC32

Header CRC32 校验值。

计算 Header CRC32 时：

```text
HeaderCRC32 = 0
```

然后对整个 4096 Byte Header 计算 CRC32。

---

# 6. DeviceType

`DeviceType` 用于区分受保护设备类型。

定义：

```go
type DeviceType uint32

const (
    DeviceTypeDisk = iota
    DeviceTypeVolume
)
```

文件格式定义：

|   值 | 含义            |
| --: | :------------ |
| `0` | Physical Disk |
| `1` | Volume        |

其他值暂未定义。

---

# 7. DeviceID

每一个 `ProtectedDevice` 记录直接保存设备唯一标识。

DeviceID 固定占用：

```text
512 Byte
```

最大有效长度：

```text
512 Byte
```

不足部分使用：

```text
0x00
```

填充。

---

## 7.1 Windows 磁盘

Windows 磁盘使用 PNPDeviceID。

例如：

```text
SCSI\DISK&VEN_INTEL&PROD_SSDSC2KB480G8\4&240534BA&0&000000
```

---

## 7.2 Windows 卷

Windows 卷使用 Volume GUID 路径。

例如：

```text
\\?\Volume{e3b9397c-0000-0000-0000-f0ff18000000}\
```

---

## 7.3 Linux 磁盘

Linux 磁盘使用稳定设备路径。

按照以下优先级选择：

```text
1. /dev/disk/by-path
2. /dev/disk/by-id
3. /dev/mapper
4. /dev
```

前一级不存在时回退到后一级。

例如：

```text
/dev/disk/by-id/nvme-xxx
```

---

## 7.4 Linux 卷

Linux 卷使用文件系统 UUID。

例如：

```text
592f36e7-ff78-41ef-8a52-ffdb62a4c57a
```

---

# 8. DiskID

`DiskID` 表示物理磁盘的唯一标识。

逻辑结构：

```go
type DiskID struct {
    ID    [DeviceIDLen]byte
    Major uint32
    Minor uint32
}
```

文件结构：

| 偏移  |   大小 | 字段      | 说明         |
| :-- | ---: | :------ | :--------- |
| 0   | 512B | `ID`    | 磁盘唯一标识     |
| 512 |   4B | `Major` | Linux 主设备号 |
| 516 |   4B | `Minor` | Linux 次设备号 |

总大小：

```text
520 Byte
```

---

## 8.1 ID

`ID` 表示磁盘唯一标识。

Windows 使用 PNPDeviceID。

Linux 使用稳定设备路径。

---

## 8.2 Major / Minor

`Major` 和 `Minor` 仅 Linux 平台有效。

Windows 平台必须填充：

```text
0
```

Linux 中：

```text
Major
```

表示设备主设备号。

```text
Minor
```

表示设备次设备号。

设备号仅用于辅助识别和校验，不作为磁盘永久唯一标识。

---

# 9. DiskExtent

`DiskExtent` 表示物理磁盘上的一段连续区域。

逻辑结构：

```go
type DiskExtent struct {
    DiskID DiskID
    Start  uint64
    Size   uint64
}
```

文件结构：

| 偏移  |   大小 | 字段       | 说明     |
| :-- | ---: | :------- | :----- |
| 0   | 520B | `DiskID` | 所属物理磁盘 |
| 520 |   8B | `Start`  | 起始偏移   |
| 528 |   8B | `Size`   | 区域长度   |

总大小：

```text
536 Byte
```

---

## 9.1 Start

物理磁盘上的起始偏移。

单位：

```text
Byte
```

---

## 9.2 Size

区域长度。

单位：

```text
Byte
```

必须满足：

```text
Size > 0
```

---

# 10. ProtectedExtent

`ProtectedExtent` 表示一个受保护的磁盘区域以及该区域对应的 Bitmap Unit 范围。

逻辑结构：

```go
type ProtectedExtent struct {
    IsValid         uint32
    Extent          DiskExtent
    BitmapUnitStart uint64
    BitmapUnitCount uint64
}
```

文件结构：

| 偏移  |   大小 | 字段                | 说明             |
| :-- | ---: | :---------------- | :------------- |
| 0   |   4B | `IsValid`         | Extent 是否有效    |
| 4   | 536B | `Extent`          | 物理磁盘区间         |
| 540 |   8B | `BitmapUnitStart` | 起始 Bitmap Unit |
| 548 |   8B | `BitmapUnitCount` | Bitmap Unit 数量 |

总大小：

```text
556 Byte
```

---

## 10.1 IsValid

表示当前 Extent 是否有效。

|   值 | 含义 |
| --: | :- |
| `0` | 无效 |
| `1` | 有效 |

其他值暂未定义。

---

## 10.2 BitmapUnitStart

表示该 Extent 使用的起始 Bitmap Unit 索引。

---

## 10.3 BitmapUnitCount

表示该 Extent 使用的 Bitmap Unit 数量。

Bitmap Unit 范围采用左闭右开区间：

```text
[BitmapUnitStart,
 BitmapUnitStart + BitmapUnitCount)
```

例如：

```text
BitmapUnitStart = 100
BitmapUnitCount = 6
```

表示使用：

```text
Bitmap Unit 100
Bitmap Unit 101
Bitmap Unit 102
Bitmap Unit 103
Bitmap Unit 104
Bitmap Unit 105
```

---

# 11. ProtectedDevice

`ProtectedDevice` 表示一个受保护的设备。

每一个 `ProtectedDevice` 对应 Protected Region 中的一条记录。

逻辑结构：

```go
type ProtectedDevice struct {
    Type     DeviceType
    DeviceID [DeviceIDLen]byte
    Extents  [128]ProtectedExtent
}
```

文件结构：

| 偏移  |         大小 | 字段         | 说明        |
| :-- | ---------: | :--------- | :-------- |
| 0   |         4B | `Type`     | 设备类型      |
| 4   |       512B | `DeviceID` | 设备唯一标识    |
| 516 | 128 × 556B | `Extents`  | 受保护磁盘区域数组 |

因此：

```text
ProtectedDeviceRecordSize =
    4
    + 512
    + 128 × 556
```

即：

```text
ProtectedDeviceRecordSize =
    71684 Byte
```

---

# 12. ProtectedDevice 的 Extents

每一个 `ProtectedDevice` 最多包含：

```text
128
```

个 Extent。

Extent 是否有效由：

```text
Extents[N].IsValid
```

决定。

例如：

```text
Extents[0].IsValid = 1
Extents[1].IsValid = 1
Extents[2].IsValid = 1
Extents[3].IsValid = 0
...
```

表示当前设备具有：

```text
3
```

个有效 Extent。

因此不需要额外保存：

```text
ExtentCount
```

---

# 13. ProtectedDevice 与 DiskExtent 的关系

`ProtectedDevice.DeviceID` 表示：

> 当前被保护的逻辑设备。

`ProtectedExtent.Extent.DiskID` 表示：

> 当前受保护区域实际所在的物理磁盘。

因此：

```text
ProtectedDevice
    │
    ├── Type
    ├── DeviceID
    │
    └── Extents
          │
          ├── DiskID
          ├── Start
          ├── Size
          ├── BitmapUnitStart
          └── BitmapUnitCount
```

例如一个卷跨越两个物理磁盘：

```text
ProtectedDevice
    Type = Volume
    DeviceID = Volume{xxxx}

    Extent #0
        DiskID = DiskA
        Start  = 100GB
        Size   = 50GB

    Extent #1
        DiskID = DiskB
        Start  = 200GB
        Size   = 30GB
```

因此可以自然支持跨磁盘卷。

---

# 14. Protected Region

Protected Region 是所有 `ProtectedDevice` 记录连续排列形成的区域。

例如：

```text
ProtectedDeviceCount = 3
```

文件布局：

```text
+--------------------------------+
│ ProtectedDevice #0             │
│ 71684 Byte                     │
+--------------------------------+
│ ProtectedDevice #1             │
│ 71684 Byte                     │
+--------------------------------+
│ ProtectedDevice #2             │
│ 71684 Byte                     │
+--------------------------------+
```

因此：

```text
ProtectedRegionSize =
    ProtectedDeviceCount
    × ProtectedDeviceRecordSize
```

---

# 15. Protected Device Record 定位

第 `N` 条 ProtectedDevice 记录的位置：

```text
ProtectedDeviceOffset =
    Header.ProtectedRegionOffset
    + N × ProtectedDeviceRecordSize
```

其中：

```text
0 <= N < Header.ProtectedDeviceCount
```

---

# 16. ProtectedDevice 校验

读取 ProtectedDevice 后必须检查：

### DeviceType

必须满足：

```text
Type == DeviceTypeDisk
||
Type == DeviceTypeVolume
```

### DeviceID

必须存在有效的 DeviceID。

DeviceID 不允许为空。

### Extent

遍历：

```text
Extents[0 ... 127]
```

仅处理：

```text
IsValid == 1
```

的 Extent。

---

# 17. Extent 校验

对于每一个有效 Extent：

```text
Extent.Size > 0
```

并且：

```text
BitmapUnitCount > 0
```

必须满足：

```text
BitmapUnitStart < TotalBitmapUnits
```

以及：

```text
BitmapUnitCount <=
    TotalBitmapUnits - BitmapUnitStart
```

---

# 18. Bitmap Unit 与磁盘空间映射

Bitmap Unit 是 CDP 元数据中管理 Dirty Bitmap 的基本分配单位。

一个 Bitmap Unit 包含：

```text
BitmapClusterSize × 8
```

个 Bitmap Bit。

默认：

```text
4096 × 8
=
32768 Bit
```

每一个 Bitmap Bit 对应：

```text
BitIndexSpace
```

大小的磁盘空间。

因此一个 Bitmap Unit 最大覆盖空间：

```text
BitmapUnitCapacity =
    BitmapClusterSize
    × 8
    × BitIndexSpace
```

默认：

```text
4096 × 8 × 4MiB
=
128GiB
```

---

# 19. Bitmap Unit Allocation Map

## 19.1 设计目的

Bitmap Unit Allocation Map 用于记录每个 Bitmap Unit 是否已经分配。

必须明确区分：

```text
Bitmap Unit Allocation State
```

和：

```text
Bitmap Unit Dirty State
```

二者含义不同。

```text
Allocation Map
    ↓
Bitmap Unit 是否已经分配

Bitmap Unit Data
    ↓
Bitmap Unit 内哪些区域为 Dirty
```

---

# 20. Bitmap Unit Allocation Map 区域

| 属性            | 值                                 |
| :------------ | :-------------------------------- |
| **起始位置**      | `Header.BitmapAllocMapOffset`     |
| **区域大小**      | `Header.BitmapAllocMapSize`       |
| **分配粒度**      | 1 Bit                             |
| **一个 Bit 对应** | 1 个 Bitmap Unit                   |
| **最小大小**      | `(TotalBitmapUnits + 7) / 8` Byte |

默认：

```text
TotalBitmapUnits = 8192
```

因此：

```text
BitmapAllocMapSize =
    (8192 + 7) / 8
    =
    1024 Byte
```

即：

```text
1 KiB
```

---

# 21. Bitmap Unit Allocation Map 数据组织

第 `N` 个 Bitmap Unit：

```text
ByteIndex = N / 8
BitIndex  = N % 8
```

对应地址：

```text
Header.BitmapAllocMapOffset
+
ByteIndex
```

对应 Bit：

```text
1 << BitIndex
```

例如：

```text
Bitmap Unit 0 → Byte 0, Bit 0
Bitmap Unit 1 → Byte 0, Bit 1
Bitmap Unit 2 → Byte 0, Bit 2
...
Bitmap Unit 7 → Byte 0, Bit 7
Bitmap Unit 8 → Byte 1, Bit 0
```

---

# 22. Bitmap Unit Allocation 状态

| Bit 值 | 含义              |
| :---- | :-------------- |
| `0`   | Bitmap Unit 未分配 |
| `1`   | Bitmap Unit 已分配 |

当：

```text
AllocationMap[N] = 0
```

时：

> 对应 Bitmap Unit Data 无效，不允许依赖其中的数据。

当：

```text
AllocationMap[N] = 1
```

时：

> 对应 Bitmap Unit Data 有效，可以读取其中的 Dirty Bitmap。

---

# 23. Bitmap Unit 分配规则

Bitmap Unit 从未分配状态变为已分配状态时：

1. 找到 Allocation Map 中值为 `0` 的 Bitmap Unit；
2. 将对应 Bitmap Unit Data 初始化为全 `0`；
3. 持久化 Bitmap Unit Data；
4. 将 Allocation Map 对应 Bit 设置为 `1`；
5. 持久化 Allocation Map；
6. 更新对应 ProtectedDevice / ProtectedExtent 元数据。

推荐持久化顺序：

```text
Bitmap Unit Data
        ↓
      Flush
        ↓
Allocation Map = 1
        ↓
      Flush
        ↓
Protected Device
```

这样可以避免出现：

```text
Allocation = 1
Bitmap Data = 未初始化
```

的不一致状态。

---

# 24. Bitmap Unit 释放规则

Bitmap Unit 不再被任何 Extent 使用时，可以释放。

释放 Bitmap Unit 时：

1. 确认没有 Extent 再引用该 Bitmap Unit；
2. 更新 ProtectedDevice / ProtectedExtent；
3. 持久化 Protected Region；
4. 将 Allocation Map 对应 Bit 清零。

即：

```text
AllocationMap[N] = 0
```

Bitmap Unit Data 在释放时不要求清零。

当 Allocation Bit 为 `0` 时：

> Bitmap Unit Data 中原有数据视为无效数据。

---

# 25. Bitmap Unit Data 区域

## 25.1 区域属性

| 属性       | 值                                      |
| :------- | :------------------------------------- |
| **起始位置** | `Header.BitmapDataOffset`              |
| **单元大小** | `Header.BitmapClusterSize`             |
| **单元数量** | `Header.TotalBitmapUnits`              |
| **区域大小** | `BitmapClusterSize × TotalBitmapUnits` |

默认：

```text
BitmapClusterSize = 4096
TotalBitmapUnits   = 8192
```

因此：

```text
BitmapDataSize =
    4096 × 8192
    =
    33,554,432 Byte
    =
    32MiB
```

---

# 26. Bitmap Unit Data 定位

第 `N` 个 Bitmap Unit：

```text
BitmapUnitOffset =
    Header.BitmapDataOffset
    +
    N × Header.BitmapClusterSize
```

其中：

```text
0 <= N < Header.TotalBitmapUnits
```

---

# 27. Bitmap Unit 内部结构

Bitmap Unit 本身没有额外 Header。

整个 Bitmap Unit 空间直接作为 Bitmap 数据。

默认：

```text
4096 Byte
```

即：

```text
4096 × 8
=
32768 Bit
```

---

# 28. Bitmap Bit 映射

整个 Bitmap Data 区域中的 Bitmap Bit 按连续编号组织。

第 `K` 个 Bitmap Bit：

```text
BitmapUnitIndex =
    K / (BitmapClusterSize × 8)

BitIndexInUnit =
    K % (BitmapClusterSize × 8)
```

对应磁盘空间：

```text
StartOffset =
    K × BitIndexSpace
```

对应空间范围：

```text
[
    K × BitIndexSpace,
    (K + 1) × BitIndexSpace
)
```

---

# 29. Bitmap Bit 语义

| Bit 值 | 含义                          |
| :---- | :-------------------------- |
| `0`   | Clean：自上次同步后没有发生写入          |
| `1`   | Dirty：对应磁盘空间发生过写入，需要 CDP 处理 |

Bitmap Unit 初始化后，其全部 Bit 必须为：

```text
0
```

---

# 30. Extent 与 Bitmap Unit 的映射

每一个有效 Extent 通过：

```text
BitmapUnitStart
BitmapUnitCount
```

描述其使用的 Bitmap Unit 范围。

例如：

```text
BitmapUnitStart = 100
BitmapUnitCount = 6
```

表示：

```text
Bitmap Unit [100, 106)
```

即：

```text
100
101
102
103
104
105
```

共：

```text
6
```

个 Bitmap Unit。

---

# 31. Bitmap Unit 容量校验

Extent 所分配的 Bitmap Unit 必须足以覆盖对应磁盘区域。

校验公式：

```text
BitmapUnitCount
    × BitmapClusterSize
    × 8
    × BitIndexSpace
>=
Extent.Size
```

如果不满足：

> 视为元数据损坏。

---

# 32. Bitmap Unit Allocation 一致性校验

对于每一个有效 Extent：

```text
BitmapUnitStart < TotalBitmapUnits
```

并且：

```text
BitmapUnitCount <=
    TotalBitmapUnits - BitmapUnitStart
```

同时：

```text
AllocationMap[
    BitmapUnitStart ...
    BitmapUnitStart + BitmapUnitCount
)
```

中的每一个 Bitmap Unit 都必须为：

```text
1
```

即：

> Extent 引用的 Bitmap Unit 必须已经分配。

---

# 33. Allocation Map 反向校验规则

不要求：

```text
Allocated Bitmap Unit
```

一定当前被某个 Extent 引用。

也就是说允许：

```text
AllocationMap[N] = 1
```

但当前没有 Extent 引用 N。

原因包括：

* Bitmap Unit 预分配；
* Extent 修改；
* Extent 删除；
* Bitmap Unit 延迟回收；
* Bitmap Unit 池管理。

因此规定：

> **有效 Extent 引用的 Bitmap Unit 必须已分配；已分配的 Bitmap Unit 不要求当前必须被 Extent 引用。**

---

# 34. Bitmap Unit 重叠规则

同一个 `ProtectedDevice` 内的不同有效 Extent：

```text
Bitmap Unit Range
```

不得重叠。

例如：

```text
Extent #0:
    [100, 110)

Extent #1:
    [110, 120)
```

合法。

但：

```text
Extent #0:
    [100, 110)

Extent #1:
    [105, 115)
```

非法。

如果不同 `ProtectedDevice` 之间需要共享 Bitmap Unit，则可以允许多个 ProtectedDevice 引用同一个 Bitmap Unit。

---

# 35. 全局容量计算

## 35.1 Bitmap Unit 总 Bit 数

```text
TotalBitmapBits =
    TotalBitmapUnits
    × BitmapClusterSize
    × 8
```

默认：

```text
8192 × 4096 × 8
=
268,435,456 Bit
```

---

## 35.2 Bitmap 总覆盖容量

```text
BitmapCapacity =
    TotalBitmapBits
    × BitIndexSpace
```

默认：

```text
8192 × 4096 × 8 × 4MiB
=
1PiB
```

因此默认配置支持：

```text
1PiB
```

的 Bitmap 地址空间。

---

# 36. Bitmap Unit 单元容量

单个 Bitmap Unit 的覆盖能力：

```text
BitmapUnitCapacity =
    BitmapClusterSize
    × 8
    × BitIndexSpace
```

默认：

```text
4096 × 8 × 4MiB
=
128GiB
```

即：

```text
1 Bitmap Unit = 128GiB
```

---

# 37. 文件区域大小计算

## 37.1 Protected Region

```text
ProtectedRegionSize =
    ProtectedDeviceCount
    × ProtectedDeviceRecordSize
```

其中：

```text
ProtectedDeviceRecordSize = 71684 Byte
```

---

## 37.2 Bitmap Allocation Map

```text
BitmapAllocMapSize >=
    (TotalBitmapUnits + 7) / 8
```

---

## 37.3 Bitmap Data

```text
BitmapDataSize =
    BitmapClusterSize
    × TotalBitmapUnits
```

---

# 38. 文件布局约束

所有区域必须满足：

```text
Header.End
<= ProtectedRegionOffset

ProtectedRegionOffset
    + ProtectedRegionSize
<= BitmapAllocMapOffset

BitmapAllocMapOffset
    + BitmapAllocMapSize
<= BitmapDataOffset

BitmapDataOffset
    + BitmapDataSize
<= FileSize
```

其中：

```text
Header.End = 4096
```

各区域不得发生重叠。

---

# 39. Header 一致性检查

读取文件后至少必须检查：

## 39.1 Signature

```text
Signature == "BIOTRKMETA"
```

---

## 39.2 Version

确认当前解析器是否支持该版本。

---

## 39.3 Header CRC32

重新计算 Header CRC32，并与：

```text
HeaderCRC32
```

比较。

计算 CRC32 时：

```text
HeaderCRC32 = 0
```

---

## 39.4 Bitmap 参数

必须满足：

```text
BitmapClusterSize > 0
TotalBitmapUnits > 0
BitIndexSpace > 0
```

---

## 39.5 Protected Region

必须满足：

```text
ProtectedRegionSize ==
    ProtectedDeviceCount
    × ProtectedDeviceRecordSize
```

---

## 39.6 Allocation Map

必须满足：

```text
BitmapAllocMapSize >=
    (TotalBitmapUnits + 7) / 8
```

---

## 39.7 Bitmap Data

必须满足：

```text
BitmapDataSize =
    BitmapClusterSize
    × TotalBitmapUnits
```

并且：

```text
BitmapDataOffset + BitmapDataSize
<= FileSize
```

---

# 40. Protected Region 一致性检查

必须满足：

```text
ProtectedRegionSize ==
    ProtectedDeviceCount
    × ProtectedDeviceRecordSize
```

第 `N` 条记录位置：

```text
ProtectedRegionOffset
+
N × ProtectedDeviceRecordSize
```

其中：

```text
0 <= N < ProtectedDeviceCount
```

不得越过：

```text
ProtectedRegionOffset + ProtectedRegionSize
```

---

# 41. ProtectedDevice 一致性检查

每一条 ProtectedDevice 必须检查：

1. `Type` 必须为合法 DeviceType；
2. `DeviceID` 必须有效；
3. 所有有效 Extent 必须满足 Extent 校验规则；
4. Bitmap Unit 范围不得越界；
5. Extent 引用的 Bitmap Unit 必须已经分配；
6. Extent 对应 Bitmap Unit 容量必须足够；
7. 同一个 ProtectedDevice 内的 Bitmap Unit 范围不得重叠。

---

# 42. 元数据损坏判定

出现以下任一情况时，应视为元数据损坏：

1. Signature 错误；
2. Version 不支持；
3. Header CRC32 校验失败；
4. Header Offset / Size 越界；
5. 文件区域发生重叠；
6. ProtectedRegionSize 与 ProtectedDeviceCount 不匹配；
7. ProtectedDevice 记录越界；
8. DeviceType 非法；
9. DeviceID 无效；
10. ProtectedExtent.IsValid 非法；
11. Extent.Size 为 0；
12. BitmapUnitCount 为 0；
13. BitmapUnitStart 超出范围；
14. BitmapUnitCount 导致 Bitmap Unit 范围越界；
15. Extent 引用未分配的 Bitmap Unit；
16. Bitmap Unit 数量不足以覆盖 Extent；
17. Bitmap Allocation Map 大小不足；
18. Bitmap Data 区域大小不足；
19. 同一个 ProtectedDevice 内 Bitmap Unit 范围发生非法重叠；
20. 其他违反本格式定义的结构约束。

---

# 43. Bitmap Unit 状态模型

Bitmap Unit 生命周期：

```text
                    ┌───────────────┐
                    │    未分配      │
                    │ Allocation=0  │
                    └───────┬───────┘
                            │
                            │ Allocate
                            ↓
                    ┌───────────────┐
                    │    已分配      │
                    │ Allocation=1  │
                    │ Bitmap有效     │
                    └───────┬───────┘
                            │
                            │ Release
                            ↓
                    ┌───────────────┐
                    │    未分配      │
                    │ Allocation=0  │
                    └───────────────┘
```

其中：

```text
Allocation = 0
```

时：

```text
Bitmap Unit Data
```

内容无效。

---

# 44. 三层数据关系

整个 Bitmap 管理体系由三层组成：

```text
┌─────────────────────────────────┐
│ ProtectedDevice                 │
│                                 │
│ DeviceID                        │
│                                 │
│ ProtectedExtent                 │
│   BitmapUnitStart               │
│   BitmapUnitCount               │
└───────────────┬─────────────────┘
                │
                │ 引用
                ↓
┌─────────────────────────────────┐
│ Bitmap Unit Allocation Map      │
│                                 │
│ 0 = 未分配                       │
│ 1 = 已分配                       │
└───────────────┬─────────────────┘
                │
                │ 定位
                ↓
┌─────────────────────────────────┐
│ Bitmap Unit Data                │
│                                 │
│ 0 = Clean                       │
│ 1 = Dirty                       │
└─────────────────────────────────┘
```

因此：

```text
ProtectedDevice
    ↓
描述“谁被保护”

ProtectedExtent
    ↓
描述“被保护设备的哪些物理磁盘区域”

Bitmap Allocation Map
    ↓
描述“哪些 Bitmap Unit 已经分配”

Bitmap Unit Data
    ↓
描述“Bitmap Unit 中哪些区域发生了写入”
```

---

# 45. 完整数据关系

```text
                    ProtectedDevice
                           │
              ┌────────────┴────────────┐
              │                         │
           DeviceID                  Extents[]
                                        │
                           ┌────────────┼────────────┐
                           │            │            │
                        DiskID        Start         Size
                           │
                           │
                           └──────┐
                                  │
                         BitmapUnitStart
                                  +
                         BitmapUnitCount
                                  │
                                  ↓
                    Bitmap Allocation Map
                                  │
                                  ↓
                         Bitmap Unit Data
                                  │
                                  ↓
                              Dirty Bit
```

---

# 46. 默认参数汇总

| 参数                           | 默认值             |
| :--------------------------- | :-------------- |
| Header Size                  | 4096B           |
| BitIndexSpace                | 4MiB            |
| BitmapClusterSize            | 4096B           |
| TotalBitmapUnits             | 8192            |
| Bitmap Unit Bits             | 32768 Bit       |
| Bitmap Unit Capacity         | 128GiB          |
| Total Bitmap Bits            | 268,435,456 Bit |
| Bitmap 总覆盖能力                 | 1PiB            |
| Bitmap Allocation Map        | 1024B           |
| Bitmap Data                  | 32MiB           |
| DeviceID                     | 512B            |
| DiskID                       | 520B            |
| DiskExtent                   | 536B            |
| ProtectedExtent              | 556B            |
| ProtectedDevice              | 71684B          |
| ProtectedDevice 最大 Extent 数量 | 128             |

---

# 47. 默认文件布局示例

假设：

```text
Header.Size             = 4096
ProtectedDeviceCount    = 2
TotalBitmapUnits        = 8192
BitmapClusterSize       = 4096
```

ProtectedDevice：

```text
ProtectedDeviceRecordSize
    = 71684 Byte
```

因此：

```text
ProtectedRegionSize
    = 2 × 71684
    = 143368 Byte
```

文件布局：

```text
Header
Offset = 0
Size   = 4096
```

Protected Device Records：

```text
Offset = 4096
Size   = 143368
```

Bitmap Allocation Map：

```text
Offset = 147464
Size   = 1024
```

Bitmap Data：

```text
Offset = 148488
Size   = 8192 × 4096
      = 33554432 Byte
      = 32MiB
```

最终：

```text
+-------------------------------+
| Header                        |
| 4096B                         |
+-------------------------------+ 4096
| ProtectedDevice #0            |
| 71684B                        |
+-------------------------------+
| ProtectedDevice #1            |
| 71684B                        |
+-------------------------------+ 147464
| Bitmap Allocation Map         |
| 1024B                         |
+-------------------------------+ 148488
| Bitmap Unit Data              |
| 32MiB                         |
+-------------------------------+
```

---

# 48. 版本兼容性

Header 中的：

```text
Version
```

用于确定文件格式版本。

对于 v1.0：

```text
Version = 0x00010000
```

解析器必须根据 Version 选择对应的数据布局。

新版本不得假设旧版本 Header 或 Record 的字段布局与当前版本一致。

---

# 49. 版本演进原则

后续版本如需增加字段：

1. 优先使用 Header Reserved 区域；
2. 不得随意改变已有字段含义；
3. 新增字段应保持明确的固定大小；
4. 修改已有字段布局必须提升 Major Version；
5. 向后兼容的新增能力可以提升 Minor Version；
6. 新版本解析器应能够明确区分旧版本与新版本；
7. 未识别的 Reserved 字段必须忽略；
8. ProtectedDevice Record 的布局发生变化时必须明确升级版本。

---

# 50. 核心设计总结

BIOTRKMETA v1.0 的核心结构为：

```text
Header
    +
ProtectedDevice[]
    +
Bitmap Allocation Map
    +
Bitmap Unit Data
```

不再存在：

```text
Device ID Mapping
VirtualID
```

每一个 `ProtectedDevice` 都是一个完整、自包含的记录：

```text
ProtectedDevice
    │
    ├── Type
    │
    ├── DeviceID
    │
    └── Extents[]
            │
            ├── DiskID
            ├── Start
            ├── Size
            ├── BitmapUnitStart
            └── BitmapUnitCount
```

其中：

```text
ProtectedDevice.DeviceID
```

表示被保护的设备。

```text
ProtectedExtent.Extent.DiskID
```

表示该 Extent 实际所在的物理磁盘。

```text
BitmapUnitStart + BitmapUnitCount
```

表示该 Extent 所使用的 Bitmap Unit 范围。

```text
Bitmap Allocation Map
```

表示 Bitmap Unit 是否已经分配。

```text
Bitmap Unit Data
```

表示 Bitmap Unit 内具体哪些区域为 Dirty。

因此整个设计形成：

```text
被保护设备
    ↓
物理磁盘区间
    ↓
Bitmap Unit 范围
    ↓
Bitmap Unit 分配状态
    ↓
Dirty Bitmap
```

该设计取消了 Device ID Mapping 和 VirtualID 的间接寻址，使每条 ProtectedDevice 记录都能够独立描述一个完整的受保护对象，同时支持：

* 物理磁盘保护；
* 卷保护；
* 跨磁盘卷；
* 一个设备包含多个物理 Extent；
* Bitmap Unit 动态分配；
* Bitmap Unit 预分配；
* Bitmap Unit 延迟回收；
* 多个设备共享 Bitmap Unit；
* 后续 Bitmap Unit 池化扩展。
