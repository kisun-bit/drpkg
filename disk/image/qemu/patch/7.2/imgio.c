#include <signal.h>
#include <stdio.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <errno.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/eventfd.h>
#include <sys/ipc.h>
#include <sys/shm.h>
#include <sys/prctl.h>
#include <stdint.h>
#include <stdbool.h>
#include <getopt.h>
#include <libgen.h>

#include "qemu/osdep.h"
#include "qemu/help-texts.h"
#include "qemu/cutils.h"
#include "qapi/error.h"
#include "qemu-io.h"
#include "qemu/error-report.h"
#include "qemu/main-loop.h"
#include "qemu/module.h"
#include "qemu/option.h"
#include "qemu/config-file.h"
#include "qemu/readline.h"
#include "qemu/log.h"
#include "qemu/sockets.h"
#include "qapi/qmp/qstring.h"
#include "qapi/qmp/qdict.h"
#include "qom/object_interfaces.h"
#include "sysemu/block-backend.h"
#include "block/block_int.h"
#include "trace/control.h"
#include "crypto/init.h"
#include "qemu-version.h"
#include "qemu/memalign.h"

// 共享内存大小和偏移量定义
#define SHM_SIZE (4 << 20)          // 4MB
#define REQUEST_OFFSET 0
#define RESPONSE_OFFSET (2 << 20)   // 2MB
#define RW_MAX_LEN (1 << 20)        // 1MB

// 请求类型
typedef enum {
    REQUEST_READ = 1,
    REQUEST_WRITE,
    REQUEST_FLUSH,
    REQUEST_CLOSE
} RequestType;

// 基础请求结构
typedef struct __attribute__((packed)) {
    uint32_t type;
    uint64_t sequence;
} ShmBaseRequest;

// 基础响应结构
typedef struct __attribute__((packed)) {
    uint32_t type;
    uint64_t sequence;
    int32_t errorCode;
} ShmBaseResponse;

// 读请求
typedef struct __attribute__((packed)) {
    ShmBaseRequest base;
    int64_t offset;
    int32_t length;
} ReadRequest;

// 读响应
typedef struct __attribute__((packed)) {
    ShmBaseResponse base;
    int32_t length;
    // 数据紧跟在结构体后面
} ReadResponse;

// 写请求
typedef struct __attribute__((packed)) {
    ShmBaseRequest base;
    int64_t offset;
    int32_t length;
    // 数据紧跟在结构体后面
} WriteRequest;

// 写响应
typedef struct __attribute__((packed)) {
    ShmBaseResponse base;
    int32_t length;
} WriteResponse;

// 刷盘请求
typedef struct __attribute__((packed)) {
    ShmBaseRequest base;
} FlushRequest;

// 刷盘响应
typedef struct __attribute__((packed)) {
    ShmBaseResponse base;
} FlushResponse;

// 关闭请求
typedef struct __attribute__((packed)) {
    ShmBaseRequest base;
} CloseRequest;

// 全局变量
static int g_event_fd_request = -1;    // 请求事件描述符（Go写入，C读取）
static int g_event_fd_response = -1;   // 响应事件描述符（C写入，Go读取）
static int g_shm_id = -1;              // 共享内存ID
static bool g_enable_debug = false;    // 调试标志
static bool g_unsafe_no_flush = false; // 不安全无刷盘标志
static void *g_shm_addr = NULL;        // 共享内存地址
static char *g_request_area = NULL;    // 请求区域
static char *g_response_area = NULL;   // 响应区域
static bool g_running = true;          // 运行标志

static BlockBackend *qemuio_blk = NULL; // QEMU存储块对象
static int64_t ImageSize = 0;           // 镜像大小
static int64_t ReadBytes = 0;
static int64_t WriteBytes = 0;

// 函数声明
void on_parent_exit(int sig);
static void debugf(const char *fmt, ...);
static void infof(const char *fmt, ...);
static void errorf(const char *fmt, ...);
static void cleanup(void);
static int init_shared_memory(void);
static int openfile(char *name);
static int handle_read_request(ReadRequest *req);
static int handle_write_request(WriteRequest *req);
static int handle_flush_request(FlushRequest *req);
static int handle_close_request(CloseRequest *req);
static int process_requests(void);

void on_parent_exit(int sig) {
    infof("Received SIGTERM (parent exited), exiting child\n");
    exit(0);
}

static void debugf(const char *fmt, ...)
{
    if (!g_enable_debug) {
        return;
    }
    va_list args;
    va_start(args, fmt);
    vprintf(fmt, args);
    va_end(args);
    fflush(stdout);
}

static void infof(const char *fmt, ...)
{
    va_list args;
    va_start(args, fmt);
    vprintf(fmt, args);
    va_end(args);
    fflush(stdout);
}

static void errorf(const char *fmt, ...)
{
    va_list args;
    va_start(args, fmt);
    vfprintf(stderr, fmt, args);
    va_end(args);
    fflush(stderr);
}

// 清理资源
static void cleanup(void) {
    if (g_event_fd_request >= 0) {
        close(g_event_fd_request);
        g_event_fd_request = -1;
    }

    if (g_event_fd_response >= 0) {
        close(g_event_fd_response);
        g_event_fd_response = -1;
    }

    if (g_shm_addr != NULL) {
        shmdt(g_shm_addr);
        g_shm_addr = NULL;
    }

    // if (g_shm_id >= 0) {
    //    shmctl(g_shm_id, IPC_RMID, NULL);
    //    g_shm_id = -1;
    // }
}

// 初始化共享内存
static int init_shared_memory(void) {
    infof("Initializing shared memory with ID: %d\n", g_shm_id);

    // 映射共享内存
    g_shm_addr = shmat(g_shm_id, NULL, 0);
    if (g_shm_addr == (void *)-1) {
        errorf("Error: Failed to attach shared memory, shmat failed for shm_id=%d: %s\n", g_shm_id, strerror(errno));
        g_shm_addr = NULL;
        return -1;
    }

    infof("Shared memory attached at address: %p\n", g_shm_addr);

    // 设置请求和响应区域指针
    g_request_area = (char *)g_shm_addr + REQUEST_OFFSET;
    g_response_area = (char *)g_shm_addr + RESPONSE_OFFSET;

    infof("Request area: %p, Response area: %p\n", g_request_area, g_response_area);

    return 0;
}

static int openfile(char *name) {
    Error *local_err = NULL;

    int flags = BDRV_O_UNMAP | BDRV_O_RDWR;
    if (g_unsafe_no_flush) {
        infof("Opening file with no-flush (unsafe) mode\n");
        // FIXME 测试发现添加BDRV_O_NO_FLUSH后，速度出现极大提升，但是后续需要搞清楚此参数的说明
        flags |= BDRV_O_NO_FLUSH;
    }

    if (qemuio_blk) {
        errorf("Error: File already opened\n");
        return 1;
    }

    qemuio_blk = blk_new_open(name, NULL, NULL, flags, &local_err);
    if (!qemuio_blk) {
        error_reportf_err(local_err, "can't open%s%s: ", name ? " device " : "", name ?: "");
        errorf("Error: can't open%s%s\n", name ? " device " : "", name ? : "");
        return 1;
    }

    blk_set_enable_write_cache(qemuio_blk, TRUE);

    return 0;
}

// 处理读请求
static int handle_read_request(ReadRequest *req) {
    int ret = 0;
    ReadResponse *resp = (ReadResponse *)g_response_area;
    char *data = (char *)(resp + 1);  // 数据紧跟在响应结构体后面

    debugf("handle_read_request: offset=%ld, length=%d\n", req->offset, req->length);

    // 初始化响应
    resp->base.type = req->base.type;
    resp->base.sequence = req->base.sequence;
    resp->base.errorCode = 0;
    resp->length = 0;

    // 检查请求长度是否合法
    if (req->length <= 0 || req->length > RW_MAX_LEN) {
        resp->base.errorCode = -EINVAL;
        errorf("Invalid length: %d\n", req->length);
        return -1;
    }

    int64_t remain = ImageSize - req->offset;
    int64_t read_bytes = (remain > req->length) ? req->length : remain;

    ret = blk_pread(qemuio_blk, (int64_t)req->offset, read_bytes, data, 0);
    if (ret < 0) {
        errorf("Error: Failed to call blk_pread: %s\n", strerror(-ret));
        return ret;
    }

    __atomic_add_fetch(&ReadBytes,  (int64_t)req->length, __ATOMIC_RELAXED);

    resp->length = read_bytes;
    debugf("Read success: resp->base.type=%u, resp->base.sequence=%lu, resp->base.errorCode=%d, resp->length=%d\n",
           resp->base.type, resp->base.sequence, resp->base.errorCode, resp->length);
    return 0;
}

// 处理写请求
static int handle_write_request(WriteRequest *req) {
    int ret = 0;
    WriteResponse *resp = (WriteResponse *)g_response_area;
    char *data = (char *)(req + 1);  // 数据紧跟在请求结构体后面

    // 初始化响应
    resp->base.type = req->base.type;
    resp->base.sequence = req->base.sequence;
    resp->base.errorCode = 0;
    resp->length = 0;

    // 检查请求长度是否合法
    if (req->length <= 0 || req->length > RW_MAX_LEN) {
        resp->base.errorCode = -EINVAL;
        errorf("Invalid length: %d\n", req->length);
        return -1;
    }

    ret = blk_pwrite(qemuio_blk, req->offset, req->length, data, 0);
    if (ret < 0) {
        errorf("Error: Failed to call blk_pwrite: %s\n", strerror(-ret));
        return ret;
    }

    __atomic_add_fetch(&WriteBytes, (int64_t)req->length, __ATOMIC_RELAXED);

    resp->length = req->length;
    return 0;
}

// 处理刷盘请求
static int handle_flush_request(FlushRequest *req) {
    FlushResponse *resp = (FlushResponse *)g_response_area;

    // 初始化响应
    resp->base.type = req->base.type;
    resp->base.sequence = req->base.sequence;
    resp->base.errorCode = 0;

    // 刷盘操作
    if (qemuio_blk) {
        debugf("FLUSH\n");
        blk_flush(qemuio_blk);
    }

    return 0;
}

// 处理关闭请求
static int handle_close_request(CloseRequest *req) {
    ShmBaseResponse *resp = (ShmBaseResponse *)g_response_area;

    // 初始化响应
    resp->type = req->base.type;
    resp->sequence = req->base.sequence;
    resp->errorCode = 0;

    // 设置退出标志
    g_running = false;

    if (qemuio_blk) {
        blk_flush(qemuio_blk);
        bdrv_drain_all();
        blk_unref(qemuio_blk);
        qemuio_blk = NULL;
        debugf("CLOSE\n");
        fflush(stdout);
    }

    return 0;
}

// 处理请求
static int process_requests(void) {
    ShmBaseRequest *base_req = (ShmBaseRequest *)g_request_area;
    uint64_t value;
    int ret = 0; // 默认成功

    infof("Entering request processing loop, waiting on fd=%d\n", g_event_fd_request);

    while (g_running) {
        // 等待事件通知（从 request eventfd 读取）
        debugf("Blocking on read(event_fd_request=%d)...\n", g_event_fd_request);

        if (read(g_event_fd_request, &value, sizeof(value)) != sizeof(value)) {
            if (errno == EINTR) {
                continue;
            }
            errorf("Error: Failed to read from request eventfd\n");
            ret = -1; // 出错
            break;
        }

        // 使用 EFD_SEMAPHORE 时，每次读取返回 1
        if (value != 1) {
            errorf("Error: eventfd returned unexpected value: %lu\n", value);
            ret = -1; // 出错
            break;
        }

        debugf("Received request: type=%u, sequence=%lu\n", base_req->type, base_req->sequence);

        // 调试：打印共享内存的原始字节
        debugf("Raw bytes from shared memory (first 24 bytes):\n    ");
        for (int i = 0; i < 24; i++) {
            debugf("%02x ", (unsigned char)g_request_area[i]);
        }
        debugf("\n");

        // 根据请求类型处理
        switch (base_req->type) {
            case REQUEST_READ:
                handle_read_request((ReadRequest *)g_request_area);
                break;

            case REQUEST_WRITE:
                handle_write_request((WriteRequest *)g_request_area);
                break;

            case REQUEST_FLUSH:
                handle_flush_request((FlushRequest *)g_request_area);
                break;

            case REQUEST_CLOSE:
                handle_close_request((CloseRequest *)g_request_area);
                ret = 0; // 正常关闭
                goto out; // 退出循环

            default:
                debugf("Unknown request type: %u\n", base_req->type);
                ((ShmBaseResponse *)g_response_area)->type = base_req->type;
                ((ShmBaseResponse *)g_response_area)->sequence = base_req->sequence;
                ((ShmBaseResponse *)g_response_area)->errorCode = -EINVAL;
                break;
        }

        // 通知请求处理完成（写入 response eventfd）
        value = 1;
        if (write(g_event_fd_response, &value, sizeof(value)) != sizeof(value)) {
            perror("Failed to write to response eventfd");
            ret = -1;
            break;
        }

        debugf("Response sent\n");
    }

out:
    infof("Exiting request processing loop with ret=%d\n", ret);
    return ret;
}


// 显示使用帮助
static void show_usage(const char *prog_name) {
    fprintf(stderr, "Usage: %s -f <file_path> -r <request_efd> -p <response_efd> -s <shmid>\n", prog_name);
    fprintf(stderr, "Options:\n");
    fprintf(stderr, "  -f, --file         File path to operate on\n");
    fprintf(stderr, "  -r, --request-efd  Request event file descriptor (Go->C)\n");
    fprintf(stderr, "  -p, --response-efd Response event file descriptor (C->Go)\n");
    fprintf(stderr, "  -s, --shmid        Shared memory ID\n");
    fprintf(stderr, "  -u, --unsafe       Use no-flush\n");
    fprintf(stderr, "  -d, --debug        Enable debug output\n");
    fprintf(stderr, "  -h, --help         Show this help message\n");
}

int main(int argc, char *argv[]) {
    int opt;
    char *file_path = NULL;
    int request_efd_value = -1;
    int response_efd_value = -1;
    int shmid_value = -1;

    infof("++++++++++ imgio ++++++++++\n");

    signal(SIGTERM, on_parent_exit);
    prctl(PR_SET_PDEATHSIG, SIGTERM);

    struct option long_options[] = {
        {"file",         required_argument, 0, 'f'},
        {"request-efd",  required_argument, 0, 'r'},
        {"response-efd", required_argument, 0, 'p'},
        {"shmid",        required_argument, 0, 's'},
        {"unsafe",       no_argument,       0, 'u'},
        {"debug",        no_argument,       0, 'd'},
        {"help",         no_argument,       0, 'h'},
        {0, 0, 0, 0}
    };

    // 解析命令行参数
    while ((opt = getopt_long(argc, argv, "f:r:p:s:udh", long_options, NULL)) != -1) {
        switch (opt) {
            case 'f':
                file_path = optarg;
                break;

            case 'r':
                request_efd_value = atoi(optarg);
                break;

            case 'p':
                response_efd_value = atoi(optarg);
                break;

            case 's':
                shmid_value = atoi(optarg);
                break;

            case 'u':
                g_unsafe_no_flush = true;
                break;

            case 'd':
                g_enable_debug = true;
                break;

            case 'h':
                show_usage(argv[0]);
                return 0;

            default:
                show_usage(argv[0]);
                return 1;
        }
    }

    // 检查必要参数
    if (file_path == NULL || request_efd_value < 0 || response_efd_value < 0) {
        show_usage(argv[0]);
        return 1;
    }

    // 注册退出清理函数
    atexit(cleanup);

    // 设置eventfd
    g_event_fd_request = request_efd_value;
    g_event_fd_response = response_efd_value;

    // 设置shmid
    g_shm_id = shmid_value;

    // 初始化共享内存
    if (init_shared_memory() != 0) {
        errorf("Failed to initialize shared memory\n");
        return 1;
    }

    infof("File: %s, RequestEFD: %d, ResponseEFD: %d, ShmID: %d, PID: %d\n",
        file_path, g_event_fd_request, g_event_fd_response, g_shm_id, getpid());

    // 初始化QEMU环境
    infof("Preparing the QEMU environment\n");

    socket_init();
    error_init(argv[0]);
    module_call_init(MODULE_INIT_TRACE);
    qemu_init_exec_dir(argv[0]);
    qcrypto_init(&error_fatal);
    module_call_init(MODULE_INIT_QOM);
    qemu_add_opts(&qemu_trace_opts);
    bdrv_init();
    qemu_init_main_loop(&error_fatal);

    if (!trace_init_backends()) {
        errorf("Error: Failed to call trace_init_backends");
        return 1;
    }
    trace_init_file();
    qemu_set_log(LOG_TRACE, &error_fatal);

    if (openfile(file_path)) {
        errorf("Error: Failed to open file by qemu");
        return 1;
    }

    ImageSize = blk_getlength(qemuio_blk);
    if (ImageSize <= 0) {
        errorf("Error: Failed to call blk_getlength");
        return 1;
    }

    infof("Size of image is %ld bytes\n", ImageSize);

    // 发送就绪信号给 Go 端
    uint64_t ready_signal = 1;
    if (write(g_event_fd_response, &ready_signal, sizeof(ready_signal)) != sizeof(ready_signal)) {
        errorf("Error: Failed to send ready signal");
        return 1;
    }
    infof("Ready signal sent to parent process\n");

    // 处理请求
    if (process_requests() < 0) {
        errorf("Error: Process requests failed\n");
        return 1;
    }

    infof("Read: %ldB, Written: %ldB\n", ReadBytes, WriteBytes);
    infof("---------- imgio ----------\n");
    return 0;
}