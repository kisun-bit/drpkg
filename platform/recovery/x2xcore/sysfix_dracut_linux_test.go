package x2xcore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dracutConfPath(root string) string {
	return filepath.Join(root, "etc/dracut.conf.d", "99-restore.conf")
}

func readDracutConf(t *testing.T, root string) string {
	t.Helper()
	bs, err := os.ReadFile(dracutConfPath(root))
	if err != nil {
		t.Fatalf("read %s: %v", dracutConfPath(root), err)
	}
	return string(bs)
}

// 框架模块（add_dracutmodules）必须被正确写出，并且重复调用不产生重复行。
func TestAddDracutModulesToDracutConf_Basic(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}

	if err := fixer.addDracutModulesToDracutConf("lvm"); err != nil {
		t.Fatalf("addDracutModulesToDracutConf: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, `add_dracutmodules+=" lvm "`) {
		t.Fatalf("missing lvm dracut module:\n%s", content)
	}

	if err := fixer.addDracutModulesToDracutConf("lvm"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	content = readDracutConf(t, root)
	if got := strings.Count(content, "add_dracutmodules+="); got != 1 {
		t.Fatalf("expected exactly one add_dracutmodules line, got %d:\n%s", got, content)
	}
}

// 已存在其他框架模块时，合并且去重；同时不破坏 add_drivers 行。
func TestAddDracutModulesToDracutConf_Merge(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}

	if err := os.MkdirAll(filepath.Dir(dracutConfPath(root)), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "add_drivers+= \" virtio virtio_scsi \"\n" +
		"add_dracutmodules+=\" dm \"\n"
	if err := os.WriteFile(dracutConfPath(root), []byte(initial), 0o644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	if err := fixer.addDracutModulesToDracutConf("lvm", "dm"); err != nil {
		t.Fatalf("addDracutModulesToDracutConf: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, "add_drivers+= \" virtio virtio_scsi \"") {
		t.Fatalf("add_drivers line was altered:\n%s", content)
	}
	if !strings.Contains(content, `add_dracutmodules+=" dm lvm "`) {
		t.Fatalf("merged dracut modules not found:\n%s", content)
	}
	if got := strings.Count(content, "add_dracutmodules+="); got != 1 {
		t.Fatalf("expected exactly one add_dracutmodules line, got %d:\n%s", got, content)
	}
}

// 内核驱动（add_drivers）走同一套合并逻辑，且重复调用不产生重复行。
func TestAddModulesToDracutConf_Drivers(t *testing.T) {
	root := t.TempDir()
	fixer := &linuxSystemFixer{offsys: offlineSystem{root: root}}

	if err := fixer.addModulesToDracutConf("btrfs"); err != nil {
		t.Fatalf("addModulesToDracutConf: %v", err)
	}

	content := readDracutConf(t, root)
	if !strings.Contains(content, `add_drivers+=" btrfs "`) {
		t.Fatalf("missing btrfs driver:\n%s", content)
	}

	if err := fixer.addModulesToDracutConf("btrfs"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	content = readDracutConf(t, root)
	if got := strings.Count(content, "add_drivers+="); got != 1 {
		t.Fatalf("expected exactly one add_drivers line, got %d:\n%s", got, content)
	}
}

// 存储栈按需注入：lvm/crypt/mdraid 走框架模块，btrfs 走内核驱动。
func TestConfigureDracutStorage(t *testing.T) {
	cases := []struct {
		name        string
		useLvm      bool
		useMdraid   bool
		useBtrfs    bool
		haveLuks    bool
		wantModules []string
		wantDrivers []string
	}{
		{name: "none"},
		{name: "lvm", useLvm: true, wantModules: []string{"lvm"}},
		{name: "crypt", haveLuks: true, wantModules: []string{"crypt"}},
		{name: "mdraid", useMdraid: true, wantModules: []string{"mdraid"}},
		{name: "btrfs", useBtrfs: true, wantDrivers: []string{"btrfs"}},
		{
			name:        "all",
			useLvm:      true,
			useMdraid:   true,
			useBtrfs:    true,
			haveLuks:    true,
			wantModules: []string{"lvm", "crypt", "mdraid"},
			wantDrivers: []string{"btrfs"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			offsys := offlineSystem{
				root:      root,
				useLvm:    c.useLvm,
				useMdraid: c.useMdraid,
				useBtrfs:  c.useBtrfs,
			}
			if c.haveLuks {
				offsys.luksDeviceList = []LuksOpenResult{{}}
			}
			fixer := &linuxSystemFixer{offsys: offsys}

			if err := fixer.configureDracutStorage(); err != nil {
				t.Fatalf("configureDracutStorage: %v", err)
			}

			if len(c.wantModules) == 0 && len(c.wantDrivers) == 0 {
				if _, err := os.Stat(dracutConfPath(root)); !os.IsNotExist(err) {
					t.Fatalf("expected no conf file, stat err=%v", err)
				}
				return
			}

			content := readDracutConf(t, root)

			if len(c.wantModules) > 0 {
				wantLine := `add_dracutmodules+=" ` + strings.Join(c.wantModules, " ") + ` "`
				if !strings.Contains(content, wantLine) {
					t.Fatalf("missing %q in:\n%s", wantLine, content)
				}
			} else if strings.Contains(content, "add_dracutmodules+=") {
				t.Fatalf("unexpected add_dracutmodules line:\n%s", content)
			}

			if len(c.wantDrivers) > 0 {
				wantLine := `add_drivers+=" ` + strings.Join(c.wantDrivers, " ") + ` "`
				if !strings.Contains(content, wantLine) {
					t.Fatalf("missing %q in:\n%s", wantLine, content)
				}
			} else if strings.Contains(content, "add_drivers+=") {
				t.Fatalf("unexpected add_drivers line:\n%s", content)
			}
		})
	}
}
