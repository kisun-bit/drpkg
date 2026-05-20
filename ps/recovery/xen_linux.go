package recovery

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// unconfigXen 移除Xen的配置
func (fixer *linuxSystemFixer) unconfigXen() error {
	logger.Debugf("unconfigXen: ++")
	defer logger.Debugf("unconfigXen: --")

	//if err := unconfigureXenFromSysconfig(fixer.offsys.root, fixer.offsys.distro.Family == LinuxFamilySUSE); err != nil {
	//	return err
	//}

	if err := changeUVPTools(fixer.offsys.root, true); err != nil {
		return err
	}

	return nil
}

func (fixer *linuxSystemFixer) configXen() error {
	logger.Debugf("configXen: ++")
	defer logger.Debugf("configXen: --")

	// TODO

	if err := changeUVPTools(fixer.offsys.root, false); err != nil {
		return err
	}

	return nil
}

func unconfigureXenFromSysconfig(rootDir string, isSUSE bool) error {
	if !isSUSE {
		return nil
	}

	var kernelConfig = filepath.Join(rootDir, "etc/sysconfig/kernel")

	content, err := os.ReadFile(kernelConfig)
	if err != nil {
		return errors.Errorf("read %s failed: %w", kernelConfig, err)
	}

	variables := map[string]struct{}{
		"INITRD_MODULES":      {},
		"DOMU_INITRD_MODULES": {},
	}

	xenModules := map[string]struct{}{
		"xennet":   {},
		"xen-vnif": {},
		"xenblk":   {},
		"xen-vbd":  {},
	}

	modified := false
	var output bytes.Buffer

	scanner := bufio.NewScanner(bytes.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// 空行/注释直接保留
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			output.WriteString(line + "\n")
			continue
		}

		// 找 key=value
		idx := strings.Index(line, "=")
		if idx == -1 {
			output.WriteString(line + "\n")
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		_, needProcess := variables[key]
		if !needProcess {
			output.WriteString(line + "\n")
			continue
		}

		// 去掉引号
		unquoted := strings.Trim(value, `"'`)

		// split modules
		fields := strings.Fields(unquoted)

		var kept []string
		removed := false

		for _, mod := range fields {
			if _, isXen := xenModules[mod]; isXen {
				removed = true
				modified = true
				continue
			}
			kept = append(kept, mod)
		}

		if removed {
			newLine := fmt.Sprintf(`%s="%s"`,
				key,
				strings.Join(kept, " "),
			)
			output.WriteString(newLine + "\n")
		} else {
			output.WriteString(line + "\n")
		}
	}

	if err = scanner.Err(); err != nil {
		return err
	}

	// 没修改直接返回
	if !modified {
		return nil
	}

	// 写回
	if err = os.WriteFile(kernelConfig, output.Bytes(), 0644); err != nil {
		return errors.Errorf("write %s failed: %w", kernelConfig, err)
	}

	return nil
}

// changeUVPTools 调整华为云的UVPTools
// 华为云主机（基于xen）在安装UVPTools时，会存在如下写入逻辑（https://github.com/UVP-Tools/SAP-HANA-Tools/blob/eeceb65c5b06a4e9283273708906fadaafdc24c9/install#L1334）：
// ###pvdriver<begin> \n\
// # Let load XENPV modules \n\
// ${BALLOONMOD}modprobe xen-balloon >/dev/null 2>&1\n\
// ${HCALLMOD}modprobe xen-hcall >/dev/null 2>&1\n\
// ${VMDQMOD}modprobe xen-vmdq >/dev/null 2>&1\n\
// ${SCSIMOD}modprobe xen-scsifront >/dev/null 2>&1\n\
// ###pvdriver<end>" $RC_SYSINIT >/dev/null 2>&1
func changeUVPTools(rootDir string, deprecated bool) error {
	logger.Debugf(
		"renameXenModules: ++ (deprecated=%v)",
		deprecated,
	)
	defer logger.Debugf("renameXenModules: --")

	modulesDir := filepath.Join(rootDir, "lib", "modules")

	var errs []error

	err := filepath.WalkDir(
		modulesDir,
		func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				logger.Warnf(
					"renameXenModules: walk %s failed: %v",
					path,
					walkErr,
				)
				errs = append(errs, walkErr)
				return nil
			}

			if d.IsDir() {
				return nil
			}

			filename := d.Name()

			var newPath string

			if deprecated {
				// 只处理 xen-hcall
				if !strings.Contains(filename, "xen-hcall") {
					return nil
				}

				// 已弃用
				if strings.HasSuffix(filename, ".deprecated") {
					return nil
				}

				newPath = path + ".deprecated"
			} else {
				// 只恢复 deprecated 文件
				if !strings.HasSuffix(
					filename,
					".deprecated",
				) {
					return nil
				}

				if !strings.Contains(filename, "xen-hcall") {
					return nil
				}

				newPath = strings.TrimSuffix(
					path,
					".deprecated",
				)
			}

			logger.Infof(
				"renameXenModules: rename xen module %s -> %s",
				path,
				newPath,
			)

			if err := os.Rename(path, newPath); err != nil {
				logger.Errorf(
					"renameXenModules: rename failed %s -> %s: %v",
					path,
					newPath,
					err,
				)
				errs = append(errs, err)
			}

			return nil
		},
	)

	if err != nil {
		return errors.Errorf(
			"walk modules dir failed: %w",
			err,
		)
	}

	if len(errs) > 0 {
		return errors.Errorf(
			"rename xen modules partially failed: %v",
			errs,
		)
	}

	return nil
}
