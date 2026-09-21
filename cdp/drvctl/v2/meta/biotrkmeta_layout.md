# BIOTRKMETA 元数据文件格式规范 v1.0

CDP 系统用于保存受保护设备、磁盘区间以及 Bitmap 元数据的文件格式。
可以落在普通文件里，也可以直接写在裸设备（如 `\\.\PHYSICALDRIVE0`）的指定偏移处。

---

## 1. 全局约定

| 项目 | 约定 |
| :--- | :--- |
| 字节序 | 小端序（Little-Endian） |
| 字符编码 | UTF-8，定长字段以 `0x00` 右填充 |
| 单位 | 所有 Offset / Size 字段单位均为 Byte |
| Header 大小 | 固定 4096 Byte |
| 对齐粒度 | 4096 Byte，作用于**区域**的起始偏移与大小 |
| 保留字段 | 必须填 `0x00`，读取方必须忽略 |
| Header 校验 | CRC32（IEEE），计算时 `HeaderCRC32` 字段自身按 `0` 处理 |
| 结构对齐 | 字段按本文定义的固定大小紧凑存储，不依赖编译器结构体对齐 |

对齐粒度作用于区域而非单条记录：`DiskBitmap` 与 `ProtectedDevice` 都是变长的，
单条记录长度不保证是扇区整数倍，因此读写都以**整个区域为单位**。
这样在 4Kn 裸设备上（Windows 要求偏移与长度都按扇区对齐）才能成立。

记：

```text
AlignUp4096(x) = (x + 4095) / 4096 × 4096
```

---

## 2. 文件布局

```text
+------------------------------------------+  0
│ Header                            4096 B │
+------------------------------------------+  BitmapAllocMapOffset
│ Bitmap Unit Allocation Map               │
+------------------------------------------+  BitmapDataOffset
│ Bitmap Unit Data                         │
+------------------------------------------+  DiskBitmapAllocRegionOffset
│ Disk Bitmap Allocation（变长）           │
+------------------------------------------+  ProtectedRegionOffset
│ Protected Device Records（变长，最后）    │
+------------------------------------------+  ProtectedRegionOffset + ProtectedRegionSize
```

五个区域首尾相接，互不重叠，起始偏移与大小都是 4096 的整数倍：

```text
BitmapAllocMapOffset        = 4096
BitmapAllocMapSize          = AlignUp4096((TotalBitmapUnits + 7) / 8)
BitmapDataOffset            = BitmapAllocMapOffset + BitmapAllocMapSize
BitmapDataSize              = BitmapClusterSize × TotalBitmapUnits
DiskBitmapAllocRegionOffset = BitmapDataOffset + BitmapDataSize
DiskBitmapAllocRegionSize   = AlignUp4096(Σ DiskBitmap[i].TotalSize)
ProtectedRegionOffset       = DiskBitmapAllocRegionOffset + DiskBitmapAllocRegionSize
ProtectedRegionSize         = AlignUp4096(Σ ProtectedDevice[i].TotalSize)

TotalSize                   = ProtectedRegionOffset + ProtectedRegionSize
```

前三个区域的大小只取决于 Header 里的 Bitmap 参数，创建时就完全确定、之后不变。
后两个变长记录区（Disk Bitmap Allocation 与 Protected Device Records）随记录
增减伸缩，但**顺序固定**：磁盘位图记录区在前、受保护设备记录区在最后。

### 2.1 记录区为什么放在最后

1. **增长不会踩到别的区域**。两个变长记录区都在位图数据区之后，任何记录区长大
   都只是把区域末尾往后推；`Disk Bitmap Allocation` 增长只会把 `ProtectedRegionOffset`
   往后推，不会覆盖 Allocation Map 与 Bitmap Unit Data。
2. **`BitmapExtents` 的解析结果稳定**。它描述的是位图数据区的物理分布，
   而位图数据区在记录区之前、创建时就已完整分配且永不移动，
   所以解析结果与记录内容无关，一次写入即可收敛。
3. **不需要容量参数**。Extents / BitmapExtents / 磁盘位图记录的数量没有格式上限，
   Header 里不必预留 `MaxXxx` 字段。

代价：单条记录的偏移要靠顺序扫描 `TotalSize` 前缀累加，不能按下标直接算。

### 2.2 创建时必须写零基础区域

不能只用 `Truncate` / `SetEndOfFile` 把文件撑大，必须把
`[offset, offset + 基础区域末尾)` 全部写零（基础区域 = Header + Allocation Map + Bitmap Unit Data）：

1. **NTFS 有效数据长度（VDL）**。仅撑大文件时，VDL 之外的簇通过文件 API 读出为 0，
   物理簇里却是旧数据。驱动按物理偏移直读元数据，两边必须一致。
2. **文件空洞**。未分配的 VCN 会让 `FSCTL_GET_RETRIEVAL_POINTERS` 返回 `LCN = -1`，
   物理区间查询就无法覆盖整个元数据区域。
3. **位图数据区必须提前落定**，`BitmapExtents` 才能一次解析收敛（见 2.1）。

记录区增长时新增部分由写入方整体落盘（记录 + 对齐补零），不留未分配簇；
区域缩小时，历史高水位以下的尾部必须一并写零，避免残留已删除记录的痕迹。

---

## 3. Header

固定 4096 Byte，位于 Offset 0。

| 偏移 | 大小 | 字段 | 说明 |
| ---: | ---: | :--- | :--- |
| 0 | 16 | `Signature` | 固定 `"BIOTRKMETA"`，右侧补 `0x00` |
| 16 | 4 | `Version` | 格式版本，`Major << 16 \| Minor`；v1.0 = `0x00010000` |
| 20 | 4 | `CDPStatus` | CDP 当前状态，见 3.1 |
| 24 | 8 | `ErrorCode` | CDP 错误码，见 3.2 |
| 32 | 8 | `BitIndexSpace` | 一个 Bitmap Bit 对应的磁盘空间大小 |
| 40 | 4 | `BitmapClusterSize` | 一个 Bitmap Unit 的数据大小 |
| 44 | 4 | `TotalBitmapUnits` | Bitmap Unit 总数量 |
| 48 | 8 | `ProtectedRegionOffset` | 受保护设备记录区起始偏移 |
| 56 | 8 | `ProtectedRegionSize` | 受保护设备记录区大小，随记录变化 |
| 64 | 4 | `ProtectedDeviceCount` | 受保护设备记录条数 |
| 68 | 4 | `WorkMode` | 驱动工作模式，见 3.3 |
| 72 | 8 | `BitmapAllocMapOffset` | Allocation Map 起始偏移 |
| 80 | 8 | `BitmapAllocMapSize` | Allocation Map 大小 |
| 88 | 8 | `BitmapDataOffset` | Bitmap Unit Data 起始偏移 |
| 96 | 8 | `DiskBitmapAllocRegionOffset` | 磁盘位图记录区起始偏移 |
| 104 | 8 | `DiskBitmapAllocRegionSize` | 磁盘位图记录区大小，随磁盘记录变化 |
| 112 | 4 | `DiskCount` | 磁盘位图记录条数 |
| 116 | 4 | `HeaderCRC32` | 整个 4096 Byte Header 的 CRC32 |
| 120 | 3976 | `Reserved` | 保留，填 `0x00` |

### 3.1 CDPStatus

| 值 | 含义 |
| ---: | :--- |
| 0 | 空闲 |
| 1 | 实时 CDP |
| 2 | 定时 CDP |
| 3 | 错误 |

### 3.2 ErrorCode

| 值 | 含义 |
| ---: | :--- |
| 0 | 正常 |
| 1 | 上一次未正常关机 |
| 其他 | 由 Windows / Linux 驱动自行定义 |

### 3.3 WorkMode

| 值 | 含义 |
| ---: | :--- |
| 0 | 普通模式 |
| 1 | Linux 启用预处理阶段拷贝数据模式 |

### 3.4 Bitmap 参数

```text
一个 Bitmap Unit 的 Bit 数   = BitmapClusterSize × 8                    默认 32768
一个 Bitmap Unit 覆盖的空间  = BitmapClusterSize × 8 × BitIndexSpace     默认 16 GiB
Bitmap Bit 总数              = TotalBitmapUnits × BitmapClusterSize × 8  默认 268,435,456
可寻址的受保护空间总量        = Bitmap Bit 总数 × BitIndexSpace          默认 128 TiB
```

默认值：`BitIndexSpace = 512 KiB`、`BitmapClusterSize = 4096`、`TotalBitmapUnits = 8192`。

第 `K` 个 Bitmap Bit 对应受保护空间 `[K × BitIndexSpace, (K+1) × BitIndexSpace)`，
落在：

```text
BitmapUnitIndex  = K / (BitmapClusterSize × 8)
BitIndexInUnit   = K % (BitmapClusterSize × 8)

BitmapUnitOffset = BitmapDataOffset + BitmapUnitIndex × BitmapClusterSize
```

---

## 4. Bitmap Unit Allocation Map

每个 Bit 对应一个 Bitmap Unit 的**分配状态**（不是 Dirty 状态）：

```text
ByteIndex = N / 8
BitIndex  = N % 8
Bit       = 1 << BitIndex

地址 = BitmapAllocMapOffset + ByteIndex
```

| Bit | 含义 |
| ---: | :--- |
| 0 | 未分配，对应 Bitmap Unit Data 内容无效，不得依赖 |
| 1 | 已分配，对应 Bitmap Unit Data 可读 |

分配一个 Bitmap Unit 的顺序：先把对应 Unit Data 清零并落盘，再置 Allocation Bit 并落盘，
最后更新记录。这样不会出现 `Allocation = 1` 但数据未初始化的中间态。

释放时只需清 Allocation Bit，Unit Data 不要求清零。

---

## 5. Bitmap Unit Data

从 `BitmapDataOffset` 起，`TotalBitmapUnits` 个连续单元，每个 `BitmapClusterSize` Byte。
单元内部没有额外头部，整个空间直接是位图。

| Bit | 含义 |
| ---: | :--- |
| 0 | Clean，自上次同步后没有写入 |
| 1 | Dirty，对应磁盘空间发生过写入 |

单元刚分配时全部 Bit 必须为 0。

**位图以物理磁盘为单位拼接**：同一个物理磁盘上的所有受保护区间（无论来自多少个
`ProtectedDevice`、多少个 `Extent`）共享一张拼接位图，按记录出现顺序首尾相接。
磁盘位图由 `DiskBitmap` 记录（见第 6 章）描述其 Bitmap Unit 范围与物理分布。

---

## 6. Disk Bitmap Allocation

`DiskCount` 条 `DiskBitmap` 变长记录首尾相接，之后是补齐到 4096 的 `0x00` 填充。

第 `N` 条记录的位置：

```text
DiskBitmapAllocRegionOffset + Σ(0..N-1) TotalSize[i]
```

每条磁盘位图记录描述**一个物理磁盘**的拼接位图：

- 该拼接位图占用的 Bitmap Unit 范围（`BitmapUnitStart` + `BitmapUnitCount`）；
- 该位图数据在物理磁盘上的分布（`BitmapExtents`，Flush 后解析）。

磁盘的拼接顺序 = 该磁盘第一次出现顺序；磁盘内 extents 顺序 =（受保护设备记录顺序，
设备内 extent 顺序）。因此同一磁盘可能对应多条来自不同设备的 extent，共享一张位图。

### 6.1 DiskBitmap

| 偏移 | 大小 | 字段 | 说明 |
| ---: | ---: | :--- | :--- |
| 0 | 4 | `TotalSize` | 记录总大小，**含自身这 4 字节** |
| 4 | 520 | `DiskID` | 物理磁盘唯一标识，见 6.5 |
| 524 | 8 | `BitmapUnitStart` | 起始 Bitmap Unit 索引 |
| 532 | 8 | `BitmapUnitCount` | Bitmap Unit 数量 |
| 540 | 4 | `BitmapExtentCount` | 紧随其后的 `DiskExtent` 数量 |
| 544 | 变长 | `BitmapExtents` | `BitmapExtentCount` 个 `DiskExtent`，紧凑排列 |

```text
TotalSize          = 544 + BitmapExtentCount × 536
DiskBitmapMinBinSize = 544
```

Bitmap Unit 范围是左闭右开区间 `[BitmapUnitStart, BitmapUnitStart + BitmapUnitCount)`，
一直占用**连续**的 Bitmap Unit。

### 6.2 磁盘位图的分配与扩容

磁盘位图以 `BitmapUnit` 为粒度的分配。一个磁盘需要的 Unit 数量：

```text
磁盘总 Bit 数   = Σ ceil(Extent[i].Size / BitIndexSpace)
需要 Unit 数     = ceil(磁盘总 Bit 数 / (BitmapClusterSize × 8))
```

当受保护区间增减导致磁盘位图大小变化时，按以下规则维护：

| 场景 | 处理 |
| :--- | :--- |
| 磁盘仍存在且现有 Unit 已足够 | **复用**：保留原 `[BitmapUnitStart, BitmapUnitCount)` 不动 |
| 磁盘扩容且尾部连续空闲 | **原地增长**：`BitmapUnitStart` 不变，`BitmapUnitCount` 增大，无需搬迁数据 |
| 磁盘扩容但尾部被其他磁盘占用 | **整体搬迁**：重新分配更大的连续区间，把幸存区间的 bit 按新旧布局迁移过去，再释放旧区间 |
| 磁盘不再被任何受保护区间引用 | **释放**：清 Allocation Bit，删除该磁盘记录 |

整体搬迁必须保留**幸存区间的位图值**：某个 extent 在新旧布局中同时存在（按
`DiskID + Start + Size` 匹配）时，其 bit 段从旧位置拷贝到新位置；新出现的区间 bit 保持 0。

### 6.3 BitmapExtents

`BitmapExtents` 就是 `DiskExtent` 数组，没有独立的类型，
描述**位图数据本身**在物理磁盘上的位置。字段含义与 `DiskExtent` 不同：

| 字段 | 常规 `DiskExtent` 里 | 在 `BitmapExtents[i]` 里 |
| :--- | :--- | :--- |
| `DiskID` | 受保护区间所在的物理磁盘 | 位图数据所在的物理磁盘 |
| `Start` | 受保护区间在该磁盘上的偏移 | 位图数据在该磁盘上的**绝对偏移** |
| `Size` | 受保护区间长度 | 这一段位图数据的长度 |

解析方式：

1. 取整个元数据区域的物理磁盘分布；
2. 裁剪出位图数据区 `[BitmapDataOffset, BitmapDataOffset + BitmapDataSize)`；
3. 对每条磁盘位图记录，把它的 `[BitmapUnitStart, BitmapUnitStart + BitmapUnitCount)`
   范围映射到裁剪后的物理段上，逐段生成一个 `DiskExtent`。

段数等于元数据区域落在位图数据区那一段的物理碎片数：
写在裸设备尾部时恒为 1 段；是普通文件时等于文件在该范围内的簇碎片数。

驱动之后直接照着这份分布写物理磁盘更新位图，不需要再做裁剪计算。

### 6.4 DiskID / DiskExtent

```text
DiskID     : ID[512] + Major(4) + Minor(4)                          = 520 Byte
DiskExtent : DiskID(520) + Start(8) + Size(8)                        = 536 Byte
```

`Major` / `Minor` 仅 Linux 有效，Windows 必须填 0。
设备号只用于辅助识别，不作为磁盘的永久唯一标识。

### 6.5 DiskID.ID 取值

| 平台 | 对象 | 取值 |
| :--- | :--- | :--- |
| Windows | 磁盘 | PNPDeviceID，如 `SCSI\DISK&VEN_INTEL&PROD_SSDSC2KB480G8\4&240534BA&0&000000` |
| Windows | 卷 | Volume GUID 路径，如 `\\?\Volume{e3b9397c-...}\` |
| Linux | 磁盘 | 稳定设备路径，按 `/dev/disk/by-path` → `/dev/disk/by-id` → `/dev/mapper` → `/dev` 优先级回退 |
| Linux | 卷 | 文件系统 UUID |

`DiskID.ID` 存的是**永久标识**，不是设备路径；要打开设备时通过
`DiskID.DevicePath()` 反查（Windows 反查 `\\.\PhysicalDriveN`，Linux 反查设备文件路径）。

---

## 7. Protected Device Records

`ProtectedDeviceCount` 条变长记录首尾相接，之后是补齐到 4096 的 `0x00` 填充。
这是**最后一个**区域。

第 `N` 条记录的位置：

```text
ProtectedRegionOffset + Σ(0..N-1) TotalSize[i]
```

推荐把整个区域一次读入内存后顺序解析，不要逐条去磁盘上定位
（单条记录长度不保证扇区对齐，裸设备上会失败）。

### 7.1 ProtectedDevice

| 偏移 | 大小 | 字段 | 说明 |
| ---: | ---: | :--- | :--- |
| 0 | 4 | `TotalSize` | 记录总大小，**含自身这 4 字节** |
| 4 | 4 | `Type` | DeviceType，见 7.3 |
| 8 | 512 | `DeviceID` | 设备唯一标识，见 7.4 |
| 520 | 4 | `ExtentCount` | 紧随其后的 `DiskExtent` 数量 |
| 524 | 变长 | `Extents` | `ExtentCount` 个 `DiskExtent`，紧凑排列 |

```text
TotalSize               = 524 + ExtentCount × 536
ProtectedDeviceMinBinSize = 524
```

位图数据已经提升为磁盘级别，因此受保护设备记录**不再携带** Bitmap Unit 范围或
BitmapExtents——它只记录受保护区间本身（`DiskExtent`），位图分配统一由
`DiskBitmap` 记录维护。

### 7.2 DeviceType

| 值 | 含义 |
| ---: | :--- |
| 0 | Physical Disk |
| 1 | Volume |

### 7.3 DeviceID 取值

`ProtectedDevice.DeviceID` 是被保护的**逻辑设备**；
`Extents[i].DiskID` 是该受保护区间实际所在的**物理磁盘**。
两者可以不同，因此天然支持跨磁盘卷：

```text
ProtectedDevice  Type = Volume, DeviceID = Volume{xxxx}
    Extent #0    DiskID = DiskA, Start = 100GB, Size = 50GB
    Extent #1    DiskID = DiskB, Start = 200GB, Size = 30GB
```

---

## 8. 默认布局

默认参数 `BitIndexSpace = 512 KiB`、`BitmapClusterSize = 4096`、`TotalBitmapUnits = 8192`：

| 区域 | Offset | Size |
| :--- | ---: | ---: |
| Header | 0 | 4096 |
| Bitmap Unit Allocation Map | 4096 | 4096 |
| Bitmap Unit Data | 8192 | 33,554,432（32 MiB） |
| Disk Bitmap Allocation | 33,562,624 | 变长，无磁盘时为 0 |
| Protected Device Records | 33,562,624 | 变长，无设备时为 0 |

创建后（尚无任何记录）`DiskBitmapAllocRegionSize = 0`、`ProtectedRegionSize = 0`，
`TotalSize = 33,562,624 Byte ≈ 32.01 MiB`。

---

## 9. 校验规则

### 9.1 Header

```text
Signature                  == "BIOTRKMETA"
Version                    受支持
HeaderCRC32                与重算结果一致（重算时该字段按 0 处理）
BitIndexSpace              > 0
BitmapClusterSize          > 0
TotalBitmapUnits           > 0

BitmapAllocMapOffset       == 4096
BitmapAllocMapSize         == AlignUp4096((TotalBitmapUnits + 7) / 8)
BitmapDataOffset           == BitmapAllocMapOffset + BitmapAllocMapSize
DiskBitmapAllocRegionOffset == BitmapDataOffset + BitmapClusterSize × TotalBitmapUnits
ProtectedRegionOffset      == DiskBitmapAllocRegionOffset + DiskBitmapAllocRegionSize

DiskBitmapAllocRegionSize  % 4096 == 0
DiskCount > 0 → DiskBitmapAllocRegionSize >= DiskCount × 544
DiskCount == 0 → DiskBitmapAllocRegionSize == 0

ProtectedRegionSize        % 4096 == 0
ProtectedDeviceCount > 0 → ProtectedRegionSize >= ProtectedDeviceCount × 524
ProtectedDeviceCount == 0 → ProtectedRegionSize == 0

ProtectedRegionOffset + ProtectedRegionSize <= 文件大小
```

裸设备上 `Stat` 返回的不是设备容量，跳过最后一条，由调用方保证预留空间足够。
各区域偏移与大小都必须是 4096 的整数倍。

### 9.2 DiskBitmap 记录

```text
TotalSize              >= 544，且不超过区域剩余字节数
实际解码消耗的字节数    == TotalSize          （多或少都算损坏）
BitmapExtentCount × 536 <= 记录剩余字节数

DiskID                  非空
BitmapUnitCount         > 0
BitmapUnitStart         <  TotalBitmapUnits
BitmapUnitCount         <= TotalBitmapUnits - BitmapUnitStart
Allocation Map 中 [BitmapUnitStart, BitmapUnitStart + BitmapUnitCount) 全部为 1
同一磁盘位图记录的 Bitmap Unit 范围互不重叠
```

### 9.3 ProtectedDevice 记录

```text
TotalSize              >= 524，且不超过区域剩余字节数
实际解码消耗的字节数    == TotalSize
ExtentCount × 536      <= 记录剩余字节数

Type                   ∈ {0, 1}
DeviceID               非空

Extent.Size            > 0
```

### 9.4 BitmapExtents

`BitmapExtentCount == 0` 表示尚未解析，跳过本节。非空时，对每一段 `DiskExtent`：

```text
DiskID                  非空
Size                    > 0
Size % BitmapClusterSize == 0
Σ Size                  == BitmapUnitCount × BitmapClusterSize
```

最后一条是硬要求：驱动照着这份分布直接写物理磁盘，
少一段会漏写位图，长度错位会写到位图数据区之外，损坏磁盘数据。
裁剪边界都落在 Bitmap Unit 边界上，因此每段长度必然是 `BitmapClusterSize` 的整数倍。

以上任一条不满足即视为元数据损坏。

---

## 10. 版本演进

`Version` 用于确定布局，解析器必须按版本选择对应的数据结构。

1. 新增字段优先使用 Header 的 `Reserved` 区域；
2. 不得改变已有字段的含义与偏移；
3. 改变已有字段布局必须提升 Major Version；
4. 向后兼容的新增能力提升 Minor Version；
5. 未识别的 `Reserved` 字段必须忽略；
6. 记录布局发生变化时必须明确升级版本。

新版本不得假设旧版本 Header 或记录的字段布局与当前版本一致。