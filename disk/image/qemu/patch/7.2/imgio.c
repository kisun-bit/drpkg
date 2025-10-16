#include <stdio.h>
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
typedef struct {
    uint32_t type;
    uint64_t sequence;
} ShmBaseRequest;

// 基础响应结构
typedef struct {
    uint32_t type;
    uint64_t sequence;
    int32_t errorCode;
} ShmBaseResponse;

// 读请求
typedef struct {
    ShmBaseRequest base;
    int64_t offset;
    int32_t length;
} ReadRequest;

// 读响应
typedef struct {
    ShmBaseResponse base;
    int32_t length;
    // 数据紧跟在结构体后面
} ReadResponse;

// 写请求
typedef struct {
    ShmBaseRequest base;
    int64_t offset;
    int32_t length;
    // 数据紧跟在结构体后面
} WriteRequest;

// 写响应
typedef struct {
    ShmBaseResponse base;
    int32_t length;
} WriteResponse;

// 刷盘请求
typedef struct {
    ShmBaseRequest base;
} FlushRequest;

// 刷盘响应
typedef struct {
    ShmBaseResponse base;
} FlushResponse;

// 关闭请求
typedef struct {
    ShmBaseResponse base;
} CloseRequest;

// 全局变量
static int g_file_fd = -1;          // 文件描述符
static int g_event_fd = -1;         // 事件描述符
static int g_shm_id = -1;           // 共享内存ID
static void *g_shm_addr = NULL;     // 共享内存地址
static char *g_request_area = NULL; // 请求区域
static char *g_response_area = NULL;// 响应区域
static bool g_running = true;       // 运行标志

// 函数声明
static void cleanup(void);
static int init_shared_memory(void);
static int handle_read_request(ReadRequest *req);
static int handle_write_request(WriteRequest *req);
static int handle_flush_request(FlushRequest *req);
static int handle_close_request(CloseRequest *req);
static void process_requests(void);

// 清理资源
static void cleanup(void) {
    if (g_file_fd >= 0) {
        close(g_file_fd);
        g_file_fd = -1;
    }

    if (g_event_fd >= 0) {
        close(g_event_fd);
        g_event_fd = -1;
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
    key_t key;
    pid_t pid = getpid();

    // 使用进程PID作为共享内存的key
    key = pid;

    // 创建共享内存
    g_shm_id = shmget(key, SHM_SIZE, IPC_CREAT | 0666);
    if (g_shm_id == -1) {
        perror("Failed to create shared memory");
        return -1;
    }

    // 映射共享内存
    g_shm_addr = shmat(g_shm_id, NULL, 0);
    if (g_shm_addr == (void *)-1) {
        perror("Failed to attach shared memory");
        g_shm_addr = NULL;
        return -1;
    }

    // 设置请求和响应区域指针
    g_request_area = (char *)g_shm_addr + REQUEST_OFFSET;
    g_response_area = (char *)g_shm_addr + RESPONSE_OFFSET;

    return 0;
}

// 处理读请求
static int handle_read_request(ReadRequest *req) {
    ReadResponse *resp = (ReadResponse *)g_response_area;
    char *data = (char *)(resp + 1);  // 数据紧跟在响应结构体后面
    ssize_t bytes_read;

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

    // 读取文件数据
    bytes_read = read(g_file_fd, data, req->length);
    if (bytes_read == -1) {
        resp->base.errorCode = -errno;
        return -1;
    }

    resp->length = bytes_read;
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

    while (g_running) {
        // 等待事件通知
        if (read(g_event_fd, &value, sizeof(value)) != sizeof(value)) {
            if (errno == EINTR) {
                continue;
            }
            perror("Failed to read from eventfd");
            break;
        }

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
                ((ShmBaseResponse *)g_response_area)->type = base_req->type;
                ((ShmBaseResponse *)g_response_area)->sequence = base_req->sequence;
                ((ShmBaseResponse *)g_response_area)->errorCode = -EINVAL;
                break;
        }

        // 通知请求处理完成
        value = 1;
        if (write(g_event_fd, &value, sizeof(value)) != sizeof(value)) {
            perror("Failed to write to eventfd");
            break;
        }
    }
}

// 显示使用帮助
static void show_usage(const char *prog_name) {
    fprintf(stderr, "Usage: %s -f <file_path> -e <eventfd>\n", prog_name);
    fprintf(stderr, "Options:\n");
    fprintf(stderr, "  -f, --file     File path to operate on\n");
    fprintf(stderr, "  -e, --eventfd  Event file descriptor for synchronization\n");
    fprintf(stderr, "  -h, --help     Show this help message\n");
}

int main(int argc, char *argv[]) {
    int opt;
    char *file_path = NULL;
    int eventfd_value = -1;

    struct option long_options[] = {
        {"file",    required_argument, 0, 'f'},
        {"eventfd", required_argument, 0, 'e'},
        {"help",    no_argument,       0, 'h'},
        {0, 0, 0, 0}
    };

    // 解析命令行参数
    while ((opt = getopt_long(argc, argv, "f:e:h", long_options, NULL)) != -1) {
        switch (opt) {
            case 'f':
                file_path = optarg;
                break;

            case 'e':
                eventfd_value = atoi(optarg);
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
    if (file_path == NULL || eventfd_value < 0) {
        show_usage(argv[0]);
        return 1;
    }

    // 注册退出清理函数
    atexit(cleanup);

    // 打开文件
    g_file_fd = open(file_path, O_RDWR | O_CREAT, 0666);
    if (g_file_fd == -1) {
        perror("Failed to open file");
        return 1;
    }

    // 设置eventfd
    g_event_fd = eventfd_value;

    // 初始化共享内存
    if (init_shared_memory() != 0) {
        fprintf(stderr, "Failed to initialize shared memory\n");
        return 1;
    }

    printf("imgio started. File: %s, EventFD: %d, ShmID: %d, PID: %d\n",
           file_path, g_event_fd, g_shm_id, getpid());

    // 处理请求
    process_requests();

    printf("imgio shutting down\n");

    return 0;
}