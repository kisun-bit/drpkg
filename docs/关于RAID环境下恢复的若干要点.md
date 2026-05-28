
# 硬件 RAID（真 RAID 卡）
```
强依赖驱动。

RAID 逻辑由阵列卡自己做（卡上芯片/固件完成）。

上层通常只看到：
一块逻辑盘（Virtual Disk）

底层实际可能是：
多块 SAS/SATA/NVMe 盘

没有阵列驱动：
系统可能完全看不到盘。

恢复难度：高。LiveCD和待恢复系统都得准备驱动文件
```

# 主板 RAID（Fake RAID / BIOS RAID）
```
轻依赖驱动。

RAID 逻辑主要由驱动做，BIOS 只是辅助。

装了驱动：
上层通常看到一块逻辑盘。

没驱动：
往往可在 BIOS 中切：
RAID -> AHCI

切换后：
上层通常能看到多块裸盘。

恢复难度：中。若无驱动文件，LiveCD和待恢复系统可在AHCI下恢复
```

Fake RAID 回切 RAID 推荐流程:
```
【Fake RAID 回切 RAID 推荐流程】

1. 进入 BIOS
   将磁盘控制器切换为：AHCI
2. 通过 LiveCD / 灾备介质恢复整机数据
   将系统恢复到目标盘（例如 Disk0）。
3. 启动系统
   确认 Windows 可以正常进入，系统完整、应用正常。
4. 安装 RAID 驱动
   确保系统已经能识别 RAID 控制器。
   
   建议验证：
   方法1：设备管理器中存在 RAID 控制器驱动。
   方法2：执行命令：
   pnputil /enum-drivers
   确认已安装相关阵列卡驱动。

5. 关机
6. 进入 BIOS
   将控制器模式从：AHCI 切换为：RAID / Dynamic Smart Array

7. 尝试创建 RAID1（也可是其他RAID类型）

   此时必须检查 RAID 管理界面是否支持以下能力：
   - Transform
   - Migrate
   - Create RAID1 from Existing Disk

   即：
   “基于已有系统盘创建镜像盘”

   目标效果：
   Disk0（已恢复系统）
   + Disk1（空盘）
   => RAID1

   并且：
   “保留 Disk0 数据，仅将数据同步到 Disk1”
   如果支持该能力：
   - 执行迁移 / 镜像
   - 等待后台同步完成
   - 重启系统
   - 验证系统正常启动
   然后进入第8步。

7.1 若不存在“保留已有数据”的能力（重要）

   如果 RAID 管理工具只有：
   Create Logical Drive
   且没有：
   - Preserve Existing Data
   - Migration / Transform
   能力，则：
   【不要创建 RAID！】
   因为：
   创建阵列很可能覆盖当前恢复盘 metadata，
   导致恢复结果失效甚至系统不可启动。
   此时说明：
   “该机型 / 当前控制器不支持从单盘无损回切 RAID”
   
   正确做法：
   1）放弃回切 RAID
   2）再次进入 BIOS
   3）将控制器切回：AHCI
   4）保持单盘运行

8. 重启系统
   验证系统正常启动。

【一句话原则】

能“从 Existing Disk 建 RAID”才允许回切 RAID；
不能就不要切，宁可继续跑 AHCI。
这是最安全的灾备恢复策略。

真正危险的步骤就是：
“切到 RAID 后直接点 Create Array”
很多人会在这里把已恢复系统直接弄没。
```


# 软件 RAID（OS RAID）
```
不依赖 RAID 卡。

RAID 逻辑由操作系统自己做。

底层：
能直接看到多块裸盘。

恢复难度：低。能看到裸盘。LiveCD和待恢复系统直接恢复即可
```