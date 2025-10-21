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
static int g_file_fd = -1;             // 文件描述符
static int g_event_fd_request = -1;    // 请求事件描述符（Go写入，C读取）
static int g_event_fd_response = -1;   // 响应事件描述符（C写入，Go读取）
static int g_shm_id = -1;              // 共享内存ID
static bool g_enable_debug = false;    // 调试标志
static void *g_shm_addr = NULL;        // 共享内存地址
static char *g_request_area = NULL;    // 请求区域
static char *g_response_area = NULL;   // 响应区域
static bool g_running = true;          // 运行标志

// 函数声明
void on_parent_exit(int sig);
static void debugf(const char *fmt, ...);
static void cleanup(void);
static int init_shared_memory(void);
static int handle_read_request(ReadRequest *req);
static int handle_write_request(WriteRequest *req);
static int handle_flush_request(FlushRequest *req);
static int handle_close_request(CloseRequest *req);
static void process_requests(void);

void on_parent_exit(int sig) {
    printf("Received SIGTERM (parent exited), exiting child.\n");
    exit(0);
}

void debugf(const char *fmt, ...)
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

// 清理资源
static void cleanup(void) {
    if (g_file_fd >= 0) {
        close(g_file_fd);
        g_file_fd = -1;
    }

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

    if (g_shm_id >= 0) {
        shmctl(g_shm_id, IPC_RMID, NULL);
        g_shm_id = -1;
    }
}

// 初始化共享内存
static int init_shared_memory(void) {
    printf("Initializing shared memory with ID: %d\n", g_shm_id);

    // 映射共享内存
    g_shm_addr = shmat(g_shm_id, NULL, 0);
    if (g_shm_addr == (void *)-1) {
        perror("Failed to attach shared memory");
        printf("shmat failed for shm_id=%d: %s\n", g_shm_id, strerror(errno));
        fflush(stdout);
        g_shm_addr = NULL;
        return -1;
    }

    printf("Shared memory attached at address: %p\n", g_shm_addr);

    // 设置请求和响应区域指针
    g_request_area = (char *)g_shm_addr + REQUEST_OFFSET;
    g_response_area = (char *)g_shm_addr + RESPONSE_OFFSET;

    printf("Request area: %p, Response area: %p\n", g_request_area, g_response_area);

    return 0;
}

// 处理读请求
static int handle_read_request(ReadRequest *req) {
    ReadResponse *resp = (ReadResponse *)g_response_area;
    char *data = (char *)(resp + 1);  // 数据紧跟在响应结构体后面
    ssize_t bytes_read;

    debugf("handle_read_request: offset=%ld, length=%d\n", req->offset, req->length);

    // 初始化响应
    resp->base.type = req->base.type;
    resp->base.sequence = req->base.sequence;
    resp->base.errorCode = 0;
    resp->length = 0;

    // 检查请求长度是否合法
    if (req->length <= 0 || req->length > RW_MAX_LEN) {
        resp->base.errorCode = -EINVAL;
        debugf("Invalid length: %d\n", req->length);
        return -1;
    }

    // 定位文件偏移
    if (lseek(g_file_fd, req->offset, SEEK_SET) == -1) {
        resp->base.errorCode = -errno;
        debugf("lseek failed: %s\n", strerror(errno));
        return -1;
    }

    // 读取文件数据
    bytes_read = read(g_file_fd, data, req->length);
    if (bytes_read == -1) {
        resp->base.errorCode = -errno;
        debugf("read failed: %s\n", strerror(errno));
        return -1;
    }

    resp->length = bytes_read;
    debugf("Read success: bytes_read=%ld, resp->base.type=%u, resp->base.sequence=%lu, resp->base.errorCode=%d, resp->length=%d\n",
           bytes_read, resp->base.type, resp->base.sequence, resp->base.errorCode, resp->length);
    return 0;
}

// 处理写请求
static int handle_write_request(WriteRequest *req) {
    WriteResponse *resp = (WriteResponse *)g_response_area;
    char *data = (char *)(req + 1);  // 数据紧跟在请求结构体后面
    ssize_t bytes_written;

    // 初始化响应
    resp->base.type = req->base.type;
    resp->base.sequence = req->base.sequence;
    resp->base.errorCode = 0;
    resp->length = 0;

    // 检查请求长度是否合法
    if (req->length <= 0 || req->length > RW_MAX_LEN) {
        resp->base.errorCode = -EINVAL;
        return -1;
    }

    // 定位文件偏移
    if (lseek(g_file_fd, req->offset, SEEK_SET) == -1) {
        resp->base.errorCode = -errno;
        return -1;
    }

    // 写入文件数据
    bytes_written = write(g_file_fd, data, req->length);
    if (bytes_written == -1) {
        resp->base.errorCode = -errno;
        return -1;
    }

    resp->length = bytes_written;
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
    if (fsync(g_file_fd) == -1) {
        resp->base.errorCode = -errno;
        return -1;
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

    return 0;
}

// 处理请求
static void process_requests(void) {
    ShmBaseRequest *base_req = (ShmBaseRequest *)g_request_area;
    uint64_t value;

    printf("Entering request processing loop, waiting on fd=%d\n", g_event_fd_request);
    fflush(stdout);

    while (g_running) {
        // 等待事件通知（从 request eventfd 读取）
        // Go 端使用 EFD_SEMAPHORE 标志，每次 read() 返回 1
        debugf("Blocking on read(event_fd_request=%d)...\n", g_event_fd_request);
        
        if (read(g_event_fd_request, &value, sizeof(value)) != sizeof(value)) {
            if (errno == EINTR) {
                continue;
            }
            perror("Failed to read from request eventfd");
            break;
        }

        // 使用 EFD_SEMAPHORE 时，每次读取返回 1
        // 如果读取到的值不是 1，说明有问题
        if (value != 1) {
            debugf("Warning: eventfd returned unexpected value: %lu\n", value);
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
                 break;

            default:
                // 未知请求类型
                printf("Unknown request type: %u\n", base_req->type);
                ((ShmBaseResponse *)g_response_area)->type = base_req->type;
                ((ShmBaseResponse *)g_response_area)->sequence = base_req->sequence;
                ((ShmBaseResponse *)g_response_area)->errorCode = -EINVAL;
                break;
        }

        // 通知请求处理完成（写入 response eventfd）
        value = 1;
        if (write(g_event_fd_response, &value, sizeof(value)) != sizeof(value)) {
            perror("Failed to write to response eventfd");
            break;
        }

        debugf("Response sent\n");

        // 处理关闭请求后，直接退出
        if (base_req->type==REQUEST_CLOSE) {
            break;
        }
    }
}

// 显示使用帮助
static void show_usage(const char *prog_name) {
    fprintf(stderr, "Usage: %s -f <file_path> -r <request_efd> -p <response_efd> -s <shmid>\n", prog_name);
    fprintf(stderr, "Options:\n");
    fprintf(stderr, "  -f, --file         File path to operate on\n");
    fprintf(stderr, "  -r, --request-efd  Request event file descriptor (Go->C)\n");
    fprintf(stderr, "  -p, --response-efd Response event file descriptor (C->Go)\n");
    fprintf(stderr, "  -s, --shmid        Shared memory ID\n");
    fprintf(stderr, "  -d, --debug        Enable debug output\n");
    fprintf(stderr, "  -h, --help         Show this help message\n");
}

int main(int argc, char *argv[]) {
    int opt;
    char *file_path = NULL;
    int request_efd_value = -1;
    int response_efd_value = -1;
    int shmid_value = -1;

    printf("++++++++++ imgio ++++++++++\n");

    signal(SIGTERM, on_parent_exit);
    prctl(PR_SET_PDEATHSIG, SIGTERM);

    struct option long_options[] = {
        {"file",         required_argument, 0, 'f'},
        {"request-efd",  required_argument, 0, 'r'},
        {"response-efd", required_argument, 0, 'p'},
        {"shmid",        required_argument, 0, 's'},
        {"debug",        no_argument,       0, 'd'},
        {"help",         no_argument,       0, 'h'},
        {0, 0, 0, 0}
    };

    // 解析命令行参数
    while ((opt = getopt_long(argc, argv, "f:r:p:s:dh", long_options, NULL)) != -1) {
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

    // 打开文件（先尝试读写模式，如果失败则尝试只读模式）
    g_file_fd = open(file_path, O_RDWR);
    if (g_file_fd == -1) {
        fprintf(stderr, "Failed to open `%s`. Error: %s\n", file_path, strerror(errno));
        return 1;
    }

    // 设置eventfd
    g_event_fd_request = request_efd_value;
    g_event_fd_response = response_efd_value;

    // 设置shmid
    g_shm_id = shmid_value;

    // 初始化共享内存
    if (init_shared_memory() != 0) {
        fprintf(stderr, "Failed to initialize shared memory\n");
        return 1;
    }

    printf("File: %s, RequestEFD: %d, ResponseEFD: %d, ShmID: %d, PID: %d\n",
           file_path, g_event_fd_request, g_event_fd_response, g_shm_id, getpid());

    // 发送就绪信号给 Go 端
    uint64_t ready_signal = 1;
    if (write(g_event_fd_response, &ready_signal, sizeof(ready_signal)) != sizeof(ready_signal)) {
        perror("Failed to send ready signal");
        return 1;
    }
    printf("Ready signal sent to parent process\n");
    fflush(stdout);

    // 处理请求
    process_requests();

    printf("---------- imgio ----------\n");
    fflush(stdout);

    return 0;
}