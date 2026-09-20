package drerr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorFormatConsistent(t *testing.T) {
	cases := []struct {
		name string
		got  func() *Error
		want string
	}{
		{"New", func() *Error { return New(NotFound, "snapshot gone") },
			"NotFound[0x210601]: snapshot gone"},
		{"Newf", func() *Error { return Newf(Storage, "read vol %d", 3) },
			"StorageException[0x210301]: read vol 3"},
		{"New-no-msg", func() *Error { return New(Canceled, "") },
			"Canceled[0x210401]"},
		{"Wrap", func() *Error { return Wrap(errors.New("EIO"), Storage, "read vol") },
			"StorageException[0x210301]: read vol: EIO"},
		{"Wrapf", func() *Error { return Wrapf(errors.New("EIO"), Storage, "read vol %d", 3) },
			"StorageException[0x210301]: read vol 3: EIO"},
		{"Wrapf-no-msg", func() *Error { return Wrapf(errors.New("EIO"), Storage, "") },
			"StorageException[0x210301]: EIO"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.got()
			if got := e.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
			if got := fmt.Sprintf("%s", e); got != tc.want {
				t.Fatalf("%%s = %q, want %q", got, tc.want)
			}
			if got := fmt.Sprintf("%v", e); got != tc.want {
				t.Fatalf("%%v = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCodeStringNameHex(t *testing.T) {
	c := SnapshotNotFound
	if got := c.Name(); got != "SnapshotNotFound" {
		t.Fatalf("Name() = %q, want %q", got, "SnapshotNotFound")
	}
	if got := c.Hex(); got != "0x210602" {
		t.Fatalf("Hex() = %q, want %q", got, "0x210602")
	}
	if got := c.String(); got != "SnapshotNotFound[0x210602]" {
		t.Fatalf("String() = %q, want %q", got, "SnapshotNotFound[0x210602]")
	}
	if got := c.Category(); got != CategoryNotFound {
		t.Fatalf("Category() = %v, want %v", got, CategoryNotFound)
	}
}

func TestUnknownCodeFallback(t *testing.T) {
	c := Code(0x21FF01) // 分类 0xFF 未注册
	if got := c.Name(); got != "Unknown-01" {
		t.Fatalf("Name() = %q, want %q", got, "Unknown-01")
	}
	if got := c.String(); got != "Unknown-01[0x21ff01]" {
		t.Fatalf("String() = %q, want %q", got, "Unknown-01[0x21ff01]")
	}
	if got := c.Category(); got != Category(0xFF) {
		t.Fatalf("Category() = %v, want %v", got, Category(0xFF))
	}
}

func TestCategoryDerivation(t *testing.T) {
	cases := []struct {
		code Code
		cat  Category
	}{
		{Internal, CategoryInternal},
		{Network, CategoryNetwork},
		{NetworkTimeout, CategoryNetwork},
		{Storage, CategoryStorage},
		{Canceled, CategoryCanceled},
		{Timeout, CategoryTimeout},
		{NotFound, CategoryNotFound},
		{AlreadyExists, CategoryAlreadyExists},
		{PermissionDenied, CategoryPermission},
		{OutOfSpace, CategoryQuota},
		{Busy, CategoryBusy},
		{Corrupted, CategoryCorrupted},
		{VersionMismatch, CategoryVersion},
		{InvalidState, CategoryState},
		{InvalidArgument, CategoryArgument},
	}
	for _, tc := range cases {
		if got := tc.code.Category(); got != tc.cat {
			t.Errorf("%s Category() = %v, want %v", tc.code.Hex(), got, tc.cat)
		}
	}
}

func TestSemanticPredicates(t *testing.T) {
	// 语义方法由分类派生，逐类验证。
	if !New(NotFound, "").IsNotFound() {
		t.Error("NotFound 应命中 IsNotFound")
	}
	if New(NotFound, "").IsTimeout() {
		t.Error("NotFound 不应命中 IsTimeout")
	}
	// 网络超时属于 Network 分类而非 Timeout 分类。
	if !New(NetworkTimeout, "").IsNetwork() {
		t.Error("NetworkTimeout 应命中 IsNetwork")
	}
	if New(NetworkTimeout, "").IsTimeout() {
		t.Error("NetworkTimeout 不应命中 IsTimeout（分类为 Network）")
	}
	if !New(Timeout, "").IsTimeout() {
		t.Error("Timeout 应命中 IsTimeout")
	}
	if !New(OutOfSpace, "").IsQuota() {
		t.Error("OutOfSpace 应命中 IsQuota")
	}
	if !New(NotConsistent, "").IsInvalidState() {
		t.Error("NotConsistent 应命中 IsInvalidState")
	}
	if !New(ChecksumMismatch, "").IsCorrupted() {
		t.Error("ChecksumMismatch 应命中 IsCorrupted")
	}
}

func TestRetryable(t *testing.T) {
	retryable := []Code{
		Network, NetworkTimeout, NetworkUnreachable,
		Timeout, Busy,
	}
	for _, c := range retryable {
		if !c.Retryable() {
			t.Errorf("%s 应可重试", c.Hex())
		}
		if !New(c, "").Retryable() {
			t.Errorf("%s 的 *Error.Retryable 应为 true", c.Hex())
		}
	}

	notRetryable := []Code{
		Internal, Canceled, NotFound, AlreadyExists,
		PermissionDenied, OutOfSpace, Corrupted,
		VersionMismatch, InvalidState, InvalidArgument,
	}
	for _, c := range notRetryable {
		if c.Retryable() {
			t.Errorf("%s 不应可重试", c.Hex())
		}
	}
}

func TestUnwrapAndErrorsIs(t *testing.T) {
	root := errors.New("root cause")
	e := Wrap(root, Storage, "read vol")

	if got := errors.Unwrap(e); got != root {
		t.Fatalf("Unwrap() = %v, want root cause", got)
	}
	if !errors.Is(e, root) {
		t.Error("errors.Is 应能命中底层 cause")
	}

	var target *Error
	if !errors.As(e, &target) {
		t.Fatal("errors.As 应能提取 *Error")
	}
	if target.Code() != Storage {
		t.Fatalf("extracted code = %s, want %s", target.Code().Hex(), Storage.Hex())
	}
}

func TestErrorsIsMatchesByCode(t *testing.T) {
	a := New(NotFound, "msg a")
	b := New(NotFound, "msg b")
	c := New(AlreadyExists, "msg c")

	if !errors.Is(a, b) {
		t.Error("相同 code 的两个异常应按 code 判定相等")
	}
	if errors.Is(a, c) {
		t.Error("不同 code 的异常不应相等")
	}
}

func TestCodeOfAndHelpers(t *testing.T) {
	root := errors.New("plain")
	w := Wrap(root, Storage, "read vol")

	if got := CodeOf(w); got != Storage {
		t.Fatalf("CodeOf(wrapped) = %s, want %s", got.Hex(), Storage.Hex())
	}
	if !IsCode(w, Storage) {
		t.Error("IsCode 应命中")
	}
	if IsCode(w, NotFound) {
		t.Error("IsCode 不应误命中其他 code")
	}
	if got := CodeOf(root); got != 0 {
		t.Fatalf("CodeOf(plain) = %v, want 0", got)
	}

	_, ok := FromError(root)
	if ok {
		t.Error("FromError(plain) 不应成功")
	}
	if e, ok := FromError(w); !ok || e.Code() != Storage {
		t.Errorf("FromError(wrapped) = %v, %v", e, ok)
	}

	if e := AsError(root); e.Code() != Internal {
		t.Fatalf("AsError(plain).Code() = %s, want %s", e.Code().Hex(), Internal.Hex())
	}
	if got := AsError(w); got != w {
		t.Error("AsError(已有*Error) 应原样返回")
	}
	if AsError(nil) != nil {
		t.Error("AsError(nil) 应为 nil")
	}
}

func TestStackTrace(t *testing.T) {
	e := NewWithStack(InternalPanic, "boom")
	if got := e.StackTrace(); got == "" {
		t.Fatal("NewWithStack 应捕获堆栈")
	}
	if got := fmt.Sprintf("%+v", e); !strings.Contains(got, "drerr") {
		t.Fatalf("%%+v 应包含堆栈帧, got %q", got)
	}
	if got := fmt.Sprintf("%v", e); strings.Contains(got, "goroutine") {
		t.Fatalf("%%v 不应包含堆栈, got %q", got)
	}

	plain := New(Internal, "no stack")
	if got := plain.StackTrace(); got != "" {
		t.Fatal("普通 New 不应捕获堆栈")
	}
	withStack := plain.WithStack()
	if withStack.StackTrace() == "" {
		t.Fatal("WithStack 应补上堆栈")
	}
	if plain.StackTrace() != "" {
		t.Fatal("WithStack 不应修改源 Error")
	}
}

func TestNilReceiver(t *testing.T) {
	var e *Error
	if e.Code() != 0 {
		t.Fatalf("nil Code() = %v, want 0", e.Code())
	}
	if e.Error() != "" {
		t.Fatalf("nil Error() = %q, want empty", e.Error())
	}
	if e.IsNotFound() || e.Retryable() {
		t.Error("nil 接收者的语义方法应返回 false")
	}
	if e.Category() != CategoryUnknown {
		t.Errorf("nil Category() = %v, want CategoryUnknown", e.Category())
	}
}
