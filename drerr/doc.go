// Package drerr 定义灾备（disaster recovery）场景下的错误码与错误类型。
//
// 设计的核心是 Code：Code 是灾备异常唯一、机器可读的标识，其上关联着异常分类、
// 可重试性等语义，并可由它派生出一系列判断方法（IsNotFound、IsTimeout、Retryable
// 等）。业务代码应优先携带、透传 Code，而不是裸的 error 字符串，这样无论异常被
// 包裹多少层，调用方都能稳定地按 code 而不是按文案做分支。
//
// 所有通过 New / Newf / Wrap / Wrapf 构造出来的 *Error，其 Error() 输出格式
// 严格一致：
//
//	<异常名>[0x<code>]: <消息>: <底层错误>
//
// 其中「: <底层错误>」仅在存在底层错误（cause）时出现；「: <消息>」在消息为空时
// 省略。这样日志、CLI 输出、监控告警拿到的字符串都是同一种形状，便于按 code 检索。
//
// 与标准库的互操作：
//
//	*Error 实现了 Unwrap，因此 errors.Is / errors.As 可以沿 cause 链遍历；
//	*Error 实现了 Is，errors.Is(err, 另一个*Error) 按 code 相等判定（而非指针相等）；
//	CodeOf / IsCode / AsError / FromError 用于在任意 error 链上按 code 提取或判断。
package drerr
