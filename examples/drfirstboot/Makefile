# ============================================================
# Makefile — drfirstboot 构建脚本 (GNU Make)
#
# drfirstboot 必须使用 Go 1.20 (go1.20 或 go1.20.x) 编译,
# 因此不纳入仓库根目录的 build.go (要求 Go >= 1.24),
# 而由本目录的脚本单独编译.
#
# 用法 (在 cmd/drfirstboot 目录执行):
#   make                          编译所有支持平台 (交叉编译)
#   make GOOS=linux               仅编译 linux 的所有架构
#   make GOOS=linux GOARCH=amd64  仅编译指定平台
#   make clean                    清理编译产物
#
# 支持平台:
#   windows: 386, amd64
#   linux:   amd64, arm64, 386, loong64
#
# 工具链查找顺序:
#   1. 环境变量 GO120 (可执行文件名或完整路径)
#   2. PATH 中第一个匹配 go1.20* 的可执行文件 (go1.20 / go1.20.x)
# 无论来源如何, 编译前都会强制校验版本为 go1.20 或 go1.20.x.
#
# 产物输出到 <仓库根>/build/drfirstboot/{OS}/{ARCH}/,
# 与根目录 build.go 的输出布局一致.
# ============================================================

.PHONY: all check-go clean

# 默认编译全部支持平台; 指定 GOOS / GOARCH 时在矩阵内过滤.
ALL_PLATFORMS := windows/386 windows/amd64 linux/amd64 linux/arm64 linux/386 linux/loong64

PLATFORMS := $(ALL_PLATFORMS)
ifneq ($(GOOS),)
PLATFORMS := $(filter $(GOOS)/%,$(PLATFORMS))
endif
ifneq ($(GOARCH),)
PLATFORMS := $(filter %/$(GOARCH),$(PLATFORMS))
endif

BUILD_ROOT := $(abspath $(CURDIR)/../../build/drfirstboot)

# 查找 Go 1.20 工具链: 优先 GO120 (环境变量或命令行),
# 否则在 PATH 中查找第一个 go1.20* 可执行文件.
ifeq ($(origin GO120),undefined)
GO120 := $(shell for d in $$(echo "$$PATH" | tr ':' ' '); do for f in "$$d"/go1.20*; do [ -f "$$f" ] && [ -x "$$f" ] && { echo "$$f"; exit 0; }; done; done)
endif

# 编译所有支持平台 (先做工具链版本检查).
all: check-go
	@if [ -z "$(PLATFORMS)" ]; then \
		echo "错误: 构建矩阵中不存在 GOOS=$(GOOS) GOARCH=$(GOARCH) 的平台"; \
		echo "支持的平台: $(ALL_PLATFORMS)"; \
		exit 1; \
	fi
	@for p in $(PLATFORMS); do \
		goos=$${p%%/*}; \
		goarch=$${p##*/}; \
		name=drfirstboot; \
		[ "$$goos" = "windows" ] && name="$$name.exe"; \
		out="$(BUILD_ROOT)/$$goos/$$goarch/$$name"; \
		mkdir -p "$$(dirname "$$out")"; \
		echo ">>> Building drfirstboot for $$goos/$$goarch ..."; \
		GOOS=$$goos GOARCH=$$goarch GO111MODULE=on GOWORK=off CGO_ENABLED=0 \
			$(GO120) build -trimpath -o "$$out" . || exit 1; \
		echo ">>> Built drfirstboot for $$goos/$$goarch => $$out"; \
	done
	@echo ">>> All builds completed successfully."

# 校验工具链: 必须是 go1.20 或 go1.20.x.
check-go:
	@if [ -z "$(GO120)" ]; then \
		echo "错误: 未找到 Go 1.20 工具链 (go1.20 或 go1.20.x)."; \
		echo "请安装后重试, 例如:"; \
		echo "  go install golang.org/dl/go1.20@latest && go1.20 download"; \
		echo "或通过 GO120=<可执行文件> 指定工具链."; \
		exit 1; \
	fi
	@GO120_VERSION="$$($(GO120) version 2>/dev/null | head -n 1)"; \
	case "$$GO120_VERSION" in \
	"go version go1.20 "*|"go version go1.20."*) \
		echo ">>> 工具链检查通过: $$GO120_VERSION";; \
	*) \
		echo "错误: 要求 Go 1.20 (go1.20 或 go1.20.x), 实际为: $${GO120_VERSION:-无法获取版本信息}"; \
		echo "当前工具链: $(GO120)"; \
		exit 1;; \
	esac

# 清理编译产物 (仅清理 drfirstboot 的输出目录).
clean:
	rm -rf "$(BUILD_ROOT)"
	@echo ">>> $(BUILD_ROOT) removed."
