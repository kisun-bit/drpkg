package xutil

import (
	"bytes"
	"net"
	"strings"
	"testing"
	"time"
)

// startFakeTelnetServer 启动一个模拟 telnet 服务端：
// 接受连接后先发送 IAC 协商与横幅，收到一行真实命令
// （以 \r\n 结尾，忽略协商应答字节）后回显结果与提示符，
// 并保持连接直到客户端关闭。返回监听地址。
func startFakeTelnetServer(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		conn, e := ln.Accept()
		if e != nil {
			return
		}
		defer conn.Close()

		// 横幅 + 服务端主动协商：WILL ECHO、DO TTYPE
		_, _ = conn.Write([]byte{telnetIAC, telnetWILL, 1})
		_, _ = conn.Write([]byte{telnetIAC, telnetDO, 24})
		_, _ = conn.Write([]byte("Welcome to fake telnet\r\n"))

		// 等待真实命令行（以 \r\n 结尾）；协商应答不含换行，会被忽略
		acc := make([]byte, 0, 128)
		buf := make([]byte, 4096)
		for !bytes.Contains(acc, []byte("\r\n")) {
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			n, e := conn.Read(buf)
			if n > 0 {
				acc = append(acc, buf[:n]...)
			}
			if e != nil {
				return
			}
		}
		_, _ = conn.Write([]byte("executed\r\nlogin: "))

		// 保持连接开放：持续读取（含协商应答），直到客户端关闭
		for {
			if _, e := conn.Read(buf); e != nil {
				return
			}
		}
	}()

	return ln.Addr().String()
}

// 协商应答：服务端发起 WILL/DO，客户端在读取流程中回 DONT/WONT。
func TestTelnetClient_NegotiationReply(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type result struct {
		data []byte
		err  error
	}
	got := make(chan result, 1)

	go func() {
		conn, e := ln.Accept()
		if e != nil {
			got <- result{nil, e}
			return
		}
		defer conn.Close()

		_, _ = conn.Write([]byte{telnetIAC, telnetWILL, 1})
		_, _ = conn.Write([]byte{telnetIAC, telnetDO, 24})

		// 读满两条应答（共 6 字节），防止 TCP 分段
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		acc := make([]byte, 0, 16)
		buf := make([]byte, 16)
		for len(acc) < 6 {
			n, e := conn.Read(buf)
			if n > 0 {
				acc = append(acc, buf[:n]...)
			}
			if e != nil {
				got <- result{acc, e}
				return
			}
		}
		got <- result{acc, nil}
	}()

	client, err := DialTelnet(ln.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// 协商应答在读取流程中生成
	if _, err := client.ReadAvailable(200 * time.Millisecond); err != nil {
		t.Fatalf("read: %v", err)
	}

	r := <-got
	if r.err != nil {
		t.Fatalf("server read: %v", r.err)
	}

	want := []byte{
		telnetIAC, telnetDONT, 1,
		telnetIAC, telnetWONT, 24,
	}
	if !bytes.Equal(r.data, want) {
		t.Fatalf("negotiation reply = %v, want %v", r.data, want)
	}
}

// 读取过滤：输出中的协商字节被剥离，横幅原样可见。
func TestTelnetClient_ReadAvailable(t *testing.T) {
	addr := startFakeTelnetServer(t)

	client, err := DialTelnet(addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	out, err := client.ReadAvailable(300 * time.Millisecond)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(out, "Welcome to fake telnet") {
		t.Fatalf("banner missing in %q", out)
	}
	// 协商字节不应出现在输出中
	if strings.ContainsRune(out, rune(telnetIAC)) {
		t.Fatalf("negotiation bytes leaked into output: %q", out)
	}
}

// 交互：发送命令后按提示符读取，输出包含回显与提示符。
func TestTelnetClient_Execute(t *testing.T) {
	addr := startFakeTelnetServer(t)

	client, err := DialTelnet(addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	// 先读完横幅
	if _, err := client.ReadAvailable(300 * time.Millisecond); err != nil {
		t.Fatalf("read banner: %v", err)
	}

	out, err := client.Execute("hello", "login: ", 3*time.Second)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out, "executed") || !strings.Contains(out, "login: ") {
		t.Fatalf("unexpected output: %q", out)
	}
}

// 便捷方法：Telnet 一次性连接、发送并读尽输出。
func TestTelnet_OneShot(t *testing.T) {
	addr := startFakeTelnetServer(t)

	out, err := Telnet(addr, 3*time.Second, "hello")
	if err != nil {
		t.Fatalf("telnet: %v", err)
	}
	if !strings.Contains(out, "Welcome to fake telnet") ||
		!strings.Contains(out, "executed") {
		t.Fatalf("unexpected output: %q", out)
	}
}

// 连接失败：拒绝连接应返回错误。
func TestDialTelnet_Refused(t *testing.T) {
	// 先监听再立即关闭，得到一个大概率无服务的端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	if _, err := DialTelnet(addr, time.Second); err == nil {
		t.Fatal("expected dial error")
	}
}
