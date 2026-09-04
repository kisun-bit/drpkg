package xutil

import (
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
)

// telnet 协议（RFC 854）中协商所需的命令子集。
const (
	telnetSE   byte = 240 // End of sub-negotiation
	telnetSB   byte = 250 // Start of sub-negotiation
	telnetWILL byte = 251 // WILL
	telnetWONT byte = 252 // WONT
	telnetDO   byte = 253 // DO
	telnetDONT byte = 254 // DONT
	telnetIAC  byte = 255 // Interpret As Command
)

// TelnetClient 是一个最小化的 telnet 协议客户端。
//
// 连接建立后自动应答服务端发起的选项协商（统一拒绝：
// 对 WILL 回 DONT，对 DO 回 WONT），并在读取时过滤
// 协商字节，调用方看到的数据流中不含协商内容。
type TelnetClient struct {
	conn    net.Conn
	timeout time.Duration

	writeMu sync.Mutex // 串行化对 conn 的写入
	pending []byte     // 上次读取残留的未处理字节（不完整的 IAC 序列）
}

// DialTelnet 连接 addr（格式 host:port），返回 telnet 客户端。
// timeout 同时用作连接超时与默认读取超时，<=0 时取 10 秒。
func DialTelnet(addr string, timeout time.Duration) (*TelnetClient, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, errors.Wrapf(err, "telnet dial %s", addr)
	}

	return &TelnetClient{conn: conn, timeout: timeout}, nil
}

// Telnet 一次性便捷方法：连接 addr，逐行发送 commands 后持续读取输出，
// 直到 quiet 时间内没有新数据（或整体超时），返回读到的全部输出。
// commands 为空时仅连接并读取（可用于探测端口上的服务横幅）。
func Telnet(addr string, timeout time.Duration, commands ...string) (string, error) {
	client, err := DialTelnet(addr, timeout)
	if err != nil {
		return "", err
	}
	defer client.Close()

	for _, cmd := range commands {
		if err := client.Send(cmd); err != nil {
			return "", err
		}
	}

	return client.ReadAvailable(timeout)
}

// Close 关闭连接。
func (c *TelnetClient) Close() error {
	return c.conn.Close()
}

// Send 发送一行数据；行尾未带换行时自动追加 "\r\n"。
func (c *TelnetClient) Send(line string) error {
	if !strings.HasSuffix(line, "\n") {
		line += "\r\n"
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	_, err := c.conn.Write([]byte(line))
	return err
}

// Execute 发送命令 cmd，并读取输出直到 prompt 出现（或超时）。
func (c *TelnetClient) Execute(cmd, prompt string, timeout time.Duration) (string, error) {
	if err := c.Send(cmd); err != nil {
		return "", err
	}
	return c.ReadUntil(prompt, timeout)
}

// ReadUntil 读取输出直到 prompt 出现或超时，返回内容包含 prompt 本身。
// 超时时返回已读到的内容与超时错误。
func (c *TelnetClient) ReadUntil(prompt string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = c.timeout
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return "", err
	}
	defer c.conn.SetReadDeadline(time.Time{})

	var sb strings.Builder
	buf := make([]byte, 4096)

	for {
		data, err := c.readFiltered(buf)
		sb.WriteString(data)
		if prompt != "" && strings.Contains(sb.String(), prompt) {
			return sb.String(), nil
		}
		if err != nil {
			return sb.String(), err
		}
	}
}

// ReadAvailable 持续读取输出，直到 quiet 时间内没有新数据
// （或整体超时），返回读到的全部输出。
// 适用于无法预知提示符、只需"读尽当前输出"的场景。
func (c *TelnetClient) ReadAvailable(quiet time.Duration) (string, error) {
	if quiet <= 0 {
		quiet = time.Second
	}

	deadline := time.Now().Add(c.timeout)
	var sb strings.Builder
	buf := make([]byte, 4096)

	for {
		wait := quiet
		if remain := time.Until(deadline); remain < wait {
			wait = remain
		}
		if wait <= 0 {
			break
		}

		if err := c.conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
			return sb.String(), err
		}

		data, err := c.readFiltered(buf)
		sb.WriteString(data)

		if err == nil {
			// 收到新数据，静默窗口重新计时
			continue
		}
		if errors.Is(err, io.EOF) {
			// 对端已关闭连接，输出读取完毕
			break
		}
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// 静默窗口内无新数据，读取结束
			break
		}
		return sb.String(), err
	}

	return sb.String(), nil
}

// readFiltered 读取一次并过滤掉 telnet 协商字节，返回纯数据。
// 末尾不完整的 IAC 序列会被保留，与下次读取的数据拼接处理。
func (c *TelnetClient) readFiltered(buf []byte) (string, error) {
	n, err := c.conn.Read(buf)

	chunk := append(c.pending, buf[:n]...)
	c.pending = nil

	if len(chunk) > 0 {
		data, rest := c.filterNegotiation(chunk)
		c.pending = rest
		return string(data), err
	}

	return "", err
}

// filterNegotiation 从数据中剥离 IAC 协商命令并应答，
// 返回纯数据部分与未处理的残留字节。
func (c *TelnetClient) filterNegotiation(data []byte) (clean []byte, rest []byte) {
	out := make([]byte, 0, len(data))

	i := 0
loop:
	for i < len(data) {
		if data[i] != telnetIAC {
			out = append(out, data[i])
			i++
			continue
		}

		if i+1 >= len(data) {
			// IAC 本身不完整，留待下次
			break
		}

		cmd := data[i+1]
		switch cmd {
		case telnetWILL, telnetDO, telnetWONT, telnetDONT:
			if i+2 >= len(data) {
				// 选项字节未到齐，留待下次
				break loop
			}
			if cmd == telnetWILL || cmd == telnetDO {
				c.replyNegotiation(cmd == telnetDO, data[i+2])
			}
			// WONT/DONT 无需应答
			i += 3
		case telnetSB:
			// 子协商：跳过直到 IAC SE
			j := i + 2
			for j+1 < len(data) {
				if data[j] == telnetIAC && data[j+1] == telnetSE {
					break
				}
				j++
			}
			if j+1 >= len(data) {
				break loop
			}
			i = j + 2
		case telnetIAC:
			// IAC IAC 表示数据中的字面 0xFF
			out = append(out, telnetIAC)
			i += 2
		default:
			i += 2
		}
	}

	if i < len(data) {
		rest = append([]byte(nil), data[i:]...)
	}
	return out, rest
}

// replyNegotiation 统一拒绝服务端的选项协商：
// 对 WILL 回 DONT，对 DO 回 WONT。
func (c *TelnetClient) replyNegotiation(isDO bool, opt byte) {
	var resp []byte
	if isDO {
		resp = []byte{telnetIAC, telnetWONT, opt}
	} else {
		resp = []byte{telnetIAC, telnetDONT, opt}
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, _ = c.conn.Write(resp)
}
