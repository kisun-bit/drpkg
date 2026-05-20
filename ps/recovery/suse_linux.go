package recovery

//
// opensuse 12.3 (使用的是grub2-efi) 迁移至kvm启动黑屏问题：
// 参考：https://forum.proxmox.com/threads/sles-11-sp-3-from-vmware-to-proxmox.21945/
// """
// Firstly, there's no need for all the disks to be IDE or SATA. Just the boot (/dev/sda) needs to be IDE to begin with.
// You can set the others to scsi if you wish, or better Virtio. Bear in mind if you change to virtio, your disks will
// change from sdb, to vdb. You'll need to change any reference to the disk in configuration
// (eg. fstab, /etc/sysconfig/bootloader, /boot/grub/menu.lst) from /dev/sdb1 to /dev/disk/by-uuid/....
//
// Once you've done this, reboot to make sure you've not broken anything. Disks should still mount.
//
// Next you need to add the kernel modules for virtio to your boot time ram disk. Edit /etc/sysconfig/kernel and add
// 'virtio_blk virtio_ring virtio virtio_net virtio_balloon virtio_pci' to your INITRD_MODULES line. This is a little
// overkill, but it will make things work. If you're using the default LSI SCSI card you can add the
// 'sym53c8xx scsi_transport_spi' to the list (not tried this but it should work).
//
// Finally run 'mkinitrd'
// """
// 实践中我按照这个办法确实成功启动了迁移后的系统，但是进入了紧急模式，在journalctl -b -p err的输出如下：
// """
// Time out waiting for device dev-disk-by-id-ata-QEMU_HARDDISK_QM00002-part1.device
// Dependency failed for /boot/efi
// ...
// """
// 最后发现是因为/etc/fstab中的原因，我将dev-disk-by-id-ata-QEMU_HARDDISK_QM00002-part1改成UUID后，便正常了
//
//
// suse linux enterprise server （sles） 11sp4（使用的是grub legacy）迁移至kvm启动黑屏问题：
// 已经将virtio virtio_ring virtio_pci virtio_blk virtio_scsi virtio_net打入了initrd中，但是无法启动。
// 根因已经清楚了，就是迁移后的MachineType使用错误，应该使用i440fx，不应该使用q35
//
