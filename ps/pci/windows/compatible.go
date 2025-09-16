package windows

/*
Windows启动相关的驱动兼容性检测办法：
1. 获取系统所有硬件PCI
2. 过滤出系统启动关键的硬件类别的PCI（如网络、存储、主板等等等等）
3. 进行不兼容PCI的检测，过程如下
* 遍历Windows/INF、Windows/System32/DriverStore目录下的每一个INF文件
    * 对每一个INF文件调用SetupAPI对其解析，遍历每一个PCI硬件的硬件号集合和兼容号集合
      * 若能够匹配（即此INF兼容此PCI），则获取驱动服务名称，并依次进行下述检查：\HKEY_LOCAL_MACHINE\SYSTEM\ControlSet001\Services下是否存在驱动服务，配置是否正确
		  * 若存在且配置正确：说明离线系统兼容此PCI
		  * 若存在且配置不正确：对其进行修复（主要修复服务的servicexxx/Start配置和servicexxx/StartOverride），说明离线系统兼容此PCI
		  * 若不存在：说明离线系统不兼容此PCI
      * 若不能够匹配：继续循环下一个INF文件.
4. 得到一组不兼容的PCI集合
5. 遍历不兼容的PCI集合，并根据离线系统版本、架构、内核版本等去驱动库中获取硬件驱动，若能够获取，则通过dism进行驱动的离线注入，注入成功后将此PCI从不兼容的PCI集合中剔除
6. 得到离线系统不兼容、驱动库也不兼容的PCI集合，若此集合长度为0, 说明离线系统已兼容此PCI集合，否则存在硬件兼容性问题，不兼容此PCI集合
**/
