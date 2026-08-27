package nbd

import (
	"testing"
)

// fakeHolders 用"设备 -> 持有者列表"映射构造一个可注入的持有者读取函数。
func fakeHolders(t *testing.T, table map[string][]string) func(string) ([]string, error) {
	t.Helper()

	return func(name string) ([]string, error) {
		return table[name], nil
	}
}

func TestBuildStackGraph_LVMOnPartition(t *testing.T) {
	// LVM 建在分区上：dm-0 只出现在分区 nbd0p1 的 holders 里，
	// 整盘设备 nbd0 的 holders 是空的——这是本包修复的核心场景。
	table := map[string][]string{
		"nbd0":   {},
		"nbd0p1": {"dm-0"},
		"dm-0":   {},
	}

	graph, err := buildStackGraph([]string{"nbd0", "nbd0p1"}, fakeHolders(t, table))
	if err != nil {
		t.Fatalf("buildStackGraph: %v", err)
	}

	if got := graph["nbd0p1"]; len(got) != 1 || got[0] != "dm-0" {
		t.Fatalf("holders of nbd0p1 = %v, want [dm-0]", got)
	}
	if got := graph["nbd0"]; len(got) != 0 {
		t.Fatalf("holders of nbd0 = %v, want empty", got)
	}
	if _, ok := graph["dm-0"]; !ok {
		t.Fatal("dm-0 missing from graph")
	}
}

func TestBuildStackGraph_LVMOnWholeDisk(t *testing.T) {
	table := map[string][]string{
		"nbd1": {"dm-2"},
		"dm-2": {},
	}

	graph, err := buildStackGraph([]string{"nbd1"}, fakeHolders(t, table))
	if err != nil {
		t.Fatalf("buildStackGraph: %v", err)
	}

	if got := graph["nbd1"]; len(got) != 1 || got[0] != "dm-2" {
		t.Fatalf("holders of nbd1 = %v, want [dm-2]", got)
	}
}

func TestBuildStackGraph_DeepStackOnPartition(t *testing.T) {
	// 分区上的加密卷之上再叠 LVM：nbd0p1 -> dm-3 -> dm-4。
	table := map[string][]string{
		"nbd0p1": {"dm-3"},
		"dm-3":   {"dm-4"},
		"dm-4":   {},
	}

	graph, err := buildStackGraph([]string{"nbd0", "nbd0p1"}, fakeHolders(t, table))
	if err != nil {
		t.Fatalf("buildStackGraph: %v", err)
	}

	for _, name := range []string{"nbd0", "nbd0p1", "dm-3", "dm-4"} {
		if _, ok := graph[name]; !ok {
			t.Fatalf("graph missing %s", name)
		}
	}
}

func TestBuildStackGraph_SharedDependency(t *testing.T) {
	// 一个 md 阵列同时使用整盘和分区（跨层依赖），不应重复展开、
	// 也不应漏掉任何设备。
	table := map[string][]string{
		"nbd0":   {"md0"},
		"nbd0p1": {"md0"},
		"md0":    {},
	}

	graph, err := buildStackGraph([]string{"nbd0", "nbd0p1"}, fakeHolders(t, table))
	if err != nil {
		t.Fatalf("buildStackGraph: %v", err)
	}

	if len(graph) != 3 {
		t.Fatalf("graph size = %d, want 3 (%v)", len(graph), graph)
	}
}

func TestOrderForRemoval(t *testing.T) {
	cases := []struct {
		name  string
		graph map[string][]string
		want  []string
	}{
		{
			name: "分区上的 LVM",
			graph: map[string][]string{
				"nbd0":   {},
				"nbd0p1": {"dm-0"},
				"dm-0":   {},
			},
			want: []string{"dm-0", "nbd0", "nbd0p1"},
		},
		{
			name: "整盘上的 LVM",
			graph: map[string][]string{
				"nbd1": {"dm-2"},
				"dm-2": {},
			},
			want: []string{"dm-2", "nbd1"},
		},
		{
			name: "分区上加密卷再叠 LVM",
			graph: map[string][]string{
				"nbd0p1": {"dm-3"},
				"dm-3":   {"dm-4"},
				"dm-4":   {},
			},
			want: []string{"dm-4", "dm-3", "nbd0p1"},
		},
		{
			name: "整盘与分区共同支撑的 md",
			graph: map[string][]string{
				"nbd0":   {"md0"},
				"nbd0p1": {"md0"},
				"md0":    {},
			},
			want: []string{"md0", "nbd0", "nbd0p1"},
		},
		{
			name:  "空图",
			graph: map[string][]string{},
			want:  []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := orderForRemoval(tc.graph)

			if len(got) != len(tc.want) {
				t.Fatalf("order = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("order = %v, want %v", got, tc.want)
				}
			}

			// 不变式：依赖边上的下层设备必须排在上层设备之后。
			rank := make(map[string]int, len(got))
			for i, name := range got {
				rank[name] = i
			}
			for lower, uppers := range tc.graph {
				for _, upper := range uppers {
					if rank[upper] >= rank[lower] {
						t.Fatalf("invalid order %v: %s (upper) must be removed before %s",
							got, upper, lower)
					}
				}
			}
		})
	}
}

func TestOrderForRemoval_CycleSafety(t *testing.T) {
	// 依赖图中出现环（理论上不应发生）时不得死循环，且结果仍
	// 覆盖全部设备。
	graph := map[string][]string{
		"a": {"b"},
		"b": {"a"},
	}

	got := orderForRemoval(graph)
	if len(got) != 2 {
		t.Fatalf("order = %v, want 2 devices", got)
	}
}

func TestPartitionSuffixRegexp(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"nbd0p1", true},
		{"nbd12p34", true},
		{"md0p1", true},
		{"nbd0", false},
		{"dm-3", false},
		{"md0", false},
		{"nbd0px", false},
	}

	for _, tc := range cases {
		if got := partitionSuffixRegexp.MatchString(tc.name); got != tc.want {
			t.Errorf("partitionSuffixRegexp.MatchString(%q) = %v, want %v",
				tc.name, got, tc.want)
		}
	}
}
