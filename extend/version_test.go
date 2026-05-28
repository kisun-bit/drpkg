package extend

import (
	"sort"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"windows-10.0.19045", "10.0.19045"},
		{"10.0...16299", "10.0.16299"},
		{"6.1", "6.1"},
		{"3", "3"},
		{"", "0"},
		{"abc", "0"},
		{"1.2.0", "1.2"},
		{"1.2.0.0", "1.2"},
	}

	for _, tt := range tests {
		got := Parse(tt.input).String()

		if got != tt.want {
			t.Fatalf(
				"Parse(%q)=%q want=%q",
				tt.input,
				got,
				tt.want,
			)
		}
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want bool
	}{
		{"1.2", "1.2", true},
		{"1.2", "1.2.0", true},
		{"1.2.0.0", "1.2", true},
		{"10.0.19045", "10.0.19045", true},
		{"1.2", "1.2.1", false},
		{"6.1", "6.2", false},
	}

	for _, tt := range tests {
		got := Parse(tt.a).Equal(Parse(tt.b))

		if got != tt.want {
			t.Fatalf(
				"%s Equal %s = %v want=%v",
				tt.a,
				tt.b,
				got,
				tt.want,
			)
		}
	}
}

func TestGreaterThan(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want bool
	}{
		{"1.2.1", "1.2", true},
		{"10.0.19045", "10.0.16299", true},
		{"1.10", "1.9", true},
		{"6.2", "6.1", true},
		{"1.2", "1.2.0", false},
		{"1.2", "1.2.1", false},
	}

	for _, tt := range tests {
		got := Parse(tt.a).GreaterThan(Parse(tt.b))

		if got != tt.want {
			t.Fatalf(
				"%s > %s = %v want=%v",
				tt.a,
				tt.b,
				got,
				tt.want,
			)
		}
	}
}

func TestLessThan(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want bool
	}{
		{"1.2", "1.2.1", true},
		{"10.0.16299", "10.0.19045", true},
		{"1.9", "1.10", true},
		{"6.1", "6.2", true},
		{"1.2", "1.2.0", false},
		{"1.2.1", "1.2", false},
	}

	for _, tt := range tests {
		got := Parse(tt.a).LessThan(Parse(tt.b))

		if got != tt.want {
			t.Fatalf(
				"%s < %s = %v want=%v",
				tt.a,
				tt.b,
				got,
				tt.want,
			)
		}
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{"1.2", "1.2", 0},
		{"1.2", "1.2.0", 0},
		{"1.2.1", "1.2", 1},
		{"1.2", "1.2.1", -1},
		{"10.0.19045", "10.0.16299", 1},
		{"6.1", "6.2", -1},
	}

	for _, tt := range tests {
		got := Parse(tt.a).Compare(Parse(tt.b))

		if got != tt.want {
			t.Fatalf(
				"Compare(%s,%s)=%d want=%d",
				tt.a,
				tt.b,
				got,
				tt.want,
			)
		}
	}
}

func TestGreaterOrEqual(t *testing.T) {
	if !Parse("1.2").GreaterOrEqual(Parse("1.2")) {
		t.Fatal("expect true")
	}

	if !Parse("1.2.1").GreaterOrEqual(Parse("1.2")) {
		t.Fatal("expect true")
	}

	if Parse("1.1").GreaterOrEqual(Parse("1.2")) {
		t.Fatal("expect false")
	}
}

func TestLessOrEqual(t *testing.T) {
	if !Parse("1.2").LessOrEqual(Parse("1.2")) {
		t.Fatal("expect true")
	}

	if !Parse("1.2").LessOrEqual(Parse("1.2.1")) {
		t.Fatal("expect true")
	}

	if Parse("1.3").LessOrEqual(Parse("1.2")) {
		t.Fatal("expect false")
	}
}

func TestIsZero(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"0", true},
		{"0.0", true},
		{"", true},
		{"abc", true},
		{"1", false},
		{"0.1", false},
	}

	for _, tt := range tests {
		got := Parse(tt.input).IsZero()

		if got != tt.want {
			t.Fatalf(
				"IsZero(%s)=%v want=%v",
				tt.input,
				got,
				tt.want,
			)
		}
	}
}

func TestSort(t *testing.T) {
	versions := Versions{
		Parse("10.0.19045"),
		Parse("6.1"),
		Parse("10.0.16299"),
		Parse("1.2"),
		Parse("1.10"),
		Parse("1.9"),
	}

	sort.Sort(versions)

	got := []string{
		versions[0].String(),
		versions[1].String(),
		versions[2].String(),
		versions[3].String(),
		versions[4].String(),
		versions[5].String(),
	}

	want := []string{
		"1.2",
		"1.9",
		"1.10",
		"6.1",
		"10.0.16299",
		"10.0.19045",
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf(
				"sort[%d]=%s want=%s",
				i,
				got[i],
				want[i],
			)
		}
	}
}

// Windows版本判断场景
func TestWindowsVersionRange(t *testing.T) {
	min := Parse("6.0")
	max := Parse("6.2")

	tests := []struct {
		version string
		want    bool
	}{
		{"6.0", true},      // Vista
		{"6.1", true},      // Win7
		{"6.1.7601", true}, // Win7 SP1
		{"6.2", true},      // Win8
		{"6.3", false},     // Win8.1
		{"10.0", false},    // Win10
		{"5.1", false},     // XP
	}

	for _, tt := range tests {
		v := Parse(tt.version)

		got := v.GreaterOrEqual(min) &&
			v.LessOrEqual(max)

		if got != tt.want {
			t.Fatalf(
				"version=%s got=%v want=%v",
				tt.version,
				got,
				tt.want,
			)
		}
	}
}
