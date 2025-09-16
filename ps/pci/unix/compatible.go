package unix

/*
Linux启动相关的驱动兼容性检测办法：

1. 获取系统所有的PCI集合
2. 获取系统所有的可启动内核列表（即/boot下同时存在System.map、sysvers、vmlinuz、initrd等文件）
3. 进行不兼容PCI检测：过程如下：
* 遍历每一个可启动的内核
	* 遍历$(chroot)/lib/modules/$(当前内核)/modules.alias
    * 解压$(chroot)/initrd-$(内核)至临时目录，遍历$(临时目录)/lib/modules/$(当前内核)/modules.alias的每一行
		* 对每一行去匹配每一个PCI，看其是否兼容
* 最终得到启动不兼容的硬件PCI集合，以及他们对应的内核版本，如：[{pci:xxx, initrd:xxx, kernel:xxx}......]
4. 从第三步获取的所有不兼容的PCI集合，获取其对应的内核映射，如：
{
    "3.10.0-1127.el7.x86_64": ["pci1", "pci2", "pci3"......],
    ...
}
5. 从第4步获取的映射中，获取所有需要修复的内核列表，查找本地（非启动）兼容的PCI硬件号，过程如下：
* 遍历每一个需要修复的内核（即第4步中结构的所有的key）
	* 遍历$(chroot)/lib/modules/$(当前内核)/modules.alias的每一行，看内核所对应的不兼容的pci号（如： ["pci1", "pci2"......]），是否被兼容
		* 若被兼容，则将驱动名补充至dracut（/etc/dracut.conf）/update-initramfs（/etc/initramfs-tools/modules）/mkinitrd（/etc/sysconfig/kerne中的INITRD_MODULES）
          然后执行更新initramfs的命令（具体见：https://support.huaweicloud.com/usermanual-ims/ims_01_0326.html）
        * 若未被兼容，则记录下来，说明本地驱动库也不兼容此PCI
* 最终得到一个过滤了本机驱动已修复的不兼容PCI集合，如：
{
    "3.10.0-1127.el7.x86_64": ["pci1", ],
    ...
}
6. 遍历第5步得到的不兼容pci，根据当前离线系统版本、架构、内核，将其发送至驱动库进行匹配，若驱动库支持，则将驱动下载到本地，depmod之后，利用相关工具注入至initramfs
7. 若最终剩余的不兼容PCI为空，说明硬件兼容性问题已经被修正了，否则抛出错误提示仍然存在兼容性问题
**/
