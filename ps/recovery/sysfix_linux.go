package recovery

// TODO
//  1. 如何挂载一个Linux的root环境？
//     a. 探测root卷（boot、home、etc、opt、usr、var、）、boot卷（grub/grub2、vmlinuz*）、efi卷（EFI）
//     b. 挂载root卷、boot卷、efi卷
//     c. 挂载 mount --bind /var %s/var
//        mount --bind /var %s/var
//		  mount --bind /dev %s/dev
//		  mount --bind /dev/pts %s/dev/pts
//		  mount -t proc procfs %s/proc
//		  mount -t sysfs sysfs %s/sys
//  2. Linux下chroot后，有时无法执行ls、grep、df等基础命令，解决办法：chroot /mnt /bin/bash -c "export PATH=/sbin:/bin:$PATH; ls"
//  3. virtio 正式进入 Linux 内核：2.6.24（2008年）
//  4. initramfs打包命令使用，（可选dracut、update-initramfs、mkinitrd）
//

type linuxFixer struct {
	bootMode string

	rootDevice string
	bootDevice string
	efiDevice  string
}
