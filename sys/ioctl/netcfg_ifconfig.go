package ioctl

import (
	"fmt"
	"github.com/elliotchance/orderedmap/v2"
	"github.com/pkg/errors"
	"net"
	"os"
	"strconv"
	"strings"
)

type IfCfgManager struct {
	path      string
	dict      *orderedmap.OrderedMap[string, string]
	cloneDict *orderedmap.OrderedMap[string, string]
}

func NewIfCfgManager(ifcfgFile string) (NetworkCfgManager, error) {
	if fi, err := os.Stat(ifcfgFile); err != nil {
		return nil, err
	} else {
		if fi.Size() == 0 {
			return nil, errors.Errorf("%s is empty, can not initialize network manager", ifcfgFile)
		}
	}
	ifMgr := new(IfCfgManager)
	ifMgr.path = ifcfgFile
	ifMgr.dict = orderedmap.NewOrderedMap[string, string]()
	ifMgr.cloneDict = orderedmap.NewOrderedMap[string, string]()
	if err := ifMgr.init(); err != nil {
		return nil, err
	}
	return ifMgr, nil
}

func (im *IfCfgManager) init() error {
	content, err := os.ReadFile(im.path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		lineKVGroup := strings.Split(line, "=")
		if len(lineKVGroup) != 2 {
			continue
		}
		if lineKVGroup[0] == "" || lineKVGroup[1] == "" {
			continue
		}
		ifCfgKey, ifCfgVal := strings.ToUpper(lineKVGroup[0]), strings.ReplaceAll(lineKVGroup[1], "\"", "")
		im.dict.Set(ifCfgKey, ifCfgVal)
	}
	for el := im.dict.Front(); el != nil; el = el.Next() {
		im.cloneDict.Set(el.Key, el.Value)
	}
	return nil
}

func (im *IfCfgManager) GetIPv4BootProto() string {
	val, ok := im.dict.Get("BOOTPROTO")
	if !ok {
		return BootProtoNone
	}
	if val != BootProtoNone && val != BootProtoStatic && val != BootProtoDHCP {
		return BootProtoNone
	}
	return val
}

func (im *IfCfgManager) GetIPv4Gateway() string {
	ip, ok := im.dict.Get("GATEWAY")
	if !ok {
		return ""
	}
	return ip
}

func (im *IfCfgManager) GetIPv4DNS() []string {
	dnsList := make([]string, 0)
	for el := im.cloneDict.Front(); el != nil; el = el.Next() {
		if strings.HasPrefix(el.Key, "DNS") {
			dnsList = append(dnsList, el.Value)
		}
	}
	return dnsList
}

func (im *IfCfgManager) RemoveAllIPv4() {
	// 1) IPADDR=192.168.1.1/24
	// 2) IPADDR=192.168.1.1
	//    PREFIXLEN=24
	// 2) IPADDR=192.168.1.1
	//    PREFIX=24
	// 4) IPADDR=192.168.1.1
	//    NETMASK=255.255.255.0
	needNoteKeys := make([]string, 0)
	for el := im.cloneDict.Front(); el != nil; el = el.Next() {
		if strings.HasPrefix(el.Key, "IPADDR") || strings.HasPrefix(el.Key, "NETMASK") || strings.HasPrefix(el.Key, "PREFIX") {
			needNoteKeys = append(needNoteKeys, el.Key)
		}
	}
	for _, k := range needNoteKeys {
		v, _ := im.cloneDict.Get(k)
		im.cloneDict.Delete(k)
		im.cloneDict.Set(fmt.Sprintf("#%s", k), v)
	}
	// TODO 移除所有IPv6
}

func (im *IfCfgManager) AddIP(ip *net.IPNet) {
	isIPV4 := ip.IP.To4() != nil
	if isIPV4 {
		im.addIPv4(ip)
		return
	}
	// TODO IPV6支持.
}

func (im *IfCfgManager) addIPv4(ip *net.IPNet) {
	newIPKey := ""
	for el := im.cloneDict.Front(); el != nil; el = el.Next() {
		k, v := el.Key, el.Value
		if !strings.HasPrefix(k, "IPADDR") {
			continue
		}
		if strings.Contains(v, "/") {
			if v == ip.String() {
				return
			}
		} else {
			if v == ip.IP.String() {
				return
			}
		}
		idxStr := strings.Replace(k, "IPADDR", "", 1)
		idxStr = strings.TrimSpace(idxStr)
		idx, e := strconv.Atoi(idxStr)
		if e != nil {
			continue
		}
		newIPKey = fmt.Sprintf("IPADDR%v", idx+1)
	}
	if newIPKey == "" {
		newIPKey = "IPADDR"
	}
	im.cloneDict.Set(newIPKey, ip.String())
}

func (im *IfCfgManager) SetGateway(ip *net.IP) {
	isIPV4 := ip.To4() != nil
	if isIPV4 {
		im.setIPV4Gateway(ip)
		return
	}
	// TODO IPV6支持.
}

func (im *IfCfgManager) setIPV4Gateway(ip *net.IP) {
	oldGatewayKey := make([]string, 0)
	for el := im.cloneDict.Front(); el != nil; el = el.Next() {
		if !strings.HasPrefix(el.Key, "GATEWAY") {
			continue
		}
		oldGatewayKey = append(oldGatewayKey, el.Key)
	}
	for _, k := range oldGatewayKey {
		oldV, _ := im.cloneDict.Get(k)
		im.cloneDict.Delete(k)
		im.cloneDict.Set(fmt.Sprintf("#%s", k), oldV)
	}
	im.cloneDict.Set("GATEWAY", ip.String())
}

func (im *IfCfgManager) SaveTo(destFile string) error {
	cfgLines := make([]string, 0)
	for el := im.cloneDict.Front(); el != nil; el = el.Next() {
		cfgLines = append(cfgLines, fmt.Sprintf("%s=%s", el.Key, el.Value))
	}
	cfgContent := strings.Join(cfgLines, "\n")
	fmt.Println(cfgContent)
	return os.WriteFile(destFile, []byte(cfgContent), 0o644)
}

func (im *IfCfgManager) SaveToSelf() error {
	return im.SaveTo(im.path)
}

func (im *IfCfgManager) IfCfgPath() string {
	return im.path
}
