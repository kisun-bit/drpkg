package recovery

import (
	"github.com/kisun-bit/drpkg/logger"
	"github.com/pkg/errors"
)

// changeXenHCallName 修改xen-hcall名称
func (fixer *linuxSystemFixer) changeXenHCallName(oldName, newName string) error {
	logger.Debugf("changeXenHCallName: ++")
	defer logger.Debugf("changeXenHCallName: --")

	//
	// TODO
	//  若目标平台是xen，那么就将xen-hcall-runstor改成xen-hcall
	//  若目标平台不是xen，那么就将xen-hcall改成xen-hcall-runstor
	//

	return errors.New("changeXenHCallName: not implemented yet")
}
