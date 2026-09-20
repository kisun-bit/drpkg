package drerr

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// Error 是灾备异常。code 是机器可读的唯一标识，msg 是人类可读的摘要，
// cause 是底层原始错误（可为 nil），stack 是在需要时捕获的调用栈（可为空）。
type Error struct {
	code  Code
	msg   string
	cause error
	stack []byte
}

// New 创建一个无底层错误的灾备异常。
func New(code Code, msg string) *Error {
	return &Error{code: code, msg: msg}
}

// Newf 创建一个无底层错误、带格式化消息的灾备异常。
func Newf(code Code, format string, args ...any) *Error {
	return &Error{code: code, msg: fmt.Sprintf(format, args...)}
}

// Wrap 用 code 包裹底层错误 err。err 为 nil 时等价于 New。
func Wrap(err error, code Code, msg string) *Error {
	return &Error{code: code, msg: msg, cause: err}
}

// Wrapf 同 Wrap，但消息走格式化。
func Wrapf(err error, code Code, format string, args ...any) *Error {
	return &Error{code: code, msg: fmt.Sprintf(format, args...), cause: err}
}

// NewWithStack 创建异常并额外捕获调用栈（内部错误定位 bug 时使用）。
func NewWithStack(code Code, msg string) *Error {
	return &Error{code: code, msg: msg, stack: debug.Stack()}
}

// WrapWithStack 包裹底层错误并额外捕获调用栈。
func WrapWithStack(err error, code Code, msg string) *Error {
	return &Error{code: code, msg: msg, cause: err, stack: debug.Stack()}
}

// Code 返回本异常的 code。
func (e *Error) Code() Code {
	if e == nil {
		return 0
	}
	return e.code
}

// Cause 返回底层错误。
func (e *Error) Cause() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Unwrap 返回底层错误，使 *Error 可被 errors.Is / errors.As / errors.Unwrap
// 沿 cause 链遍历。
func (e *Error) Unwrap() error { return e.Cause() }

// Is 让 errors.Is(err, target) 在 target 为 *Error 时按 code 相等判定，而非
// 指针相等：code 是灾备异常的身份标识，同一 code 视为同类异常。
func (e *Error) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	if t, ok := target.(*Error); ok {
		return t.code == e.code
	}
	return false
}

// Error 返回统一的、可读的错误字符串，格式固定为：
//
//	<异常名>[0x<code>]: <消息>
//	<异常名>[0x<code>]: <消息>: <底层错误>
//
// 其中「: <消息>」在消息为空时省略，「: <底层错误>」在无 cause 时省略。
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	var b []byte
	b = append(b, e.code.String()...)
	if e.msg != "" {
		b = append(b, ": "...)
		b = append(b, e.msg...)
	}
	if e.cause != nil {
		b = append(b, ": "...)
		b = append(b, e.cause.Error()...)
	}
	return string(b)
}

// Format 实现 fmt.Formatter：%v / %s 输出 Error() 的简洁形式；%+v 在末尾追加
// 捕获的调用栈（若存在）。
func (e *Error) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		fmt.Fprint(s, e.Error())
		if s.Flag('+') && len(e.stack) > 0 {
			fmt.Fprintf(s, "\n%s", e.stack)
		}
	case 's':
		fmt.Fprint(s, e.Error())
	case 'q':
		fmt.Fprintf(s, "%q", e.Error())
	default:
		fmt.Fprintf(s, "%%!%c(drerr.Error=%s)", verb, e.Error())
	}
}

// StackTrace 返回捕获的调用栈（若未捕获则为空字符串）。
func (e *Error) StackTrace() string {
	if e == nil {
		return ""
	}
	return string(e.stack)
}

// WithStack 返回一个携带调用栈的副本；已有栈时原样返回。
func (e *Error) WithStack() *Error {
	if e == nil {
		return nil
	}
	if len(e.stack) > 0 {
		return e
	}
	c := *e
	c.stack = debug.Stack()
	return &c
}

// FromError 从任意 error 链中提取 *Error；未找到返回 false。
func FromError(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// AsError 把任意 error 规整为 *Error：nil 返回 nil；本身已是 *Error 则原样
// 返回；普通 error 被包装为 code=Internal 的灾备异常。
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	if e, ok := FromError(err); ok {
		return e
	}
	return &Error{code: Internal, msg: err.Error(), cause: err}
}

// CodeOf 返回 err 链中第一个 *Error 的 code；未找到返回 0。
func CodeOf(err error) Code {
	if e, ok := FromError(err); ok {
		return e.code
	}
	return 0
}

// IsCode 判断 err 链中是否存在 code 对应的灾备异常。等价于 errors.Is 的
// code 语义版本，但无需先构造 *Error 目标。
func IsCode(err error, code Code) bool {
	return CodeOf(err) == code
}

// Retryable 报告本异常是否可重试，由 code 派生。
func (e *Error) Retryable() bool { return e != nil && e.code.Retryable() }

// is 判断 e 是否属于指定分类；nil 接收者返回 false。
func (e *Error) is(c Category) bool { return e != nil && e.Category() == c }

// Category 返回本异常的异常分类；nil 返回 CategoryUnknown。
func (e *Error) Category() Category {
	if e == nil {
		return CategoryUnknown
	}
	return e.code.Category()
}

// 以下为一组灾备语义判断方法，均由 code 的分类派生，命名保持 Is* 风格。

func (e *Error) IsInternal() bool         { return e.is(CategoryInternal) }
func (e *Error) IsNetwork() bool          { return e.is(CategoryNetwork) }
func (e *Error) IsStorage() bool          { return e.is(CategoryStorage) }
func (e *Error) IsCanceled() bool         { return e.is(CategoryCanceled) }
func (e *Error) IsTimeout() bool          { return e.is(CategoryTimeout) }
func (e *Error) IsNotFound() bool         { return e.is(CategoryNotFound) }
func (e *Error) IsAlreadyExists() bool    { return e.is(CategoryAlreadyExists) }
func (e *Error) IsPermissionDenied() bool { return e.is(CategoryPermission) }
func (e *Error) IsQuota() bool            { return e.is(CategoryQuota) }
func (e *Error) IsBusy() bool             { return e.is(CategoryBusy) }
func (e *Error) IsCorrupted() bool        { return e.is(CategoryCorrupted) }
func (e *Error) IsVersionError() bool     { return e.is(CategoryVersion) }
func (e *Error) IsInvalidState() bool     { return e.is(CategoryState) }
func (e *Error) IsInvalidArgument() bool  { return e.is(CategoryArgument) }
