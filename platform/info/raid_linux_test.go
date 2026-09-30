package info

import "testing"

func TestParseMdRaidUUID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			// IMSM external metadata 阵列的真实 mdadm --detail --export 输出。
			name: "imsm container member",
			in: "MD_LEVEL=raid0\n" +
				"MD_DEVICES=5\n" +
				"MD_CONTAINER=/dev/md/imsm0\n" +
				"MD_MEMBER=0\n" +
				"MD_UUID=1d53fd99:78696630:3673e26b:6a44304c\n" +
				"MD_DEVNAME=Volume0\n" +
				"MD_DEVICE_ev_sda_ROLE=0\n",
			want: "1d53fd99:78696630:3673e26b:6a44304c",
		},
		{
			name: "line surrounded by whitespace",
			in:   "MD_LEVEL=raid1\n   MD_UUID=deadbeef:00000000:00000000:00000001   \n",
			want: "deadbeef:00000000:00000000:00000001",
		},
		{
			name: "no uuid line",
			in:   "MD_LEVEL=raid0\nMD_DEVICES=5\n",
			want: "",
		},
		{
			name: "empty uuid value",
			in:   "MD_UUID=\n",
			want: "",
		},
		{
			name: "empty output",
			in:   "",
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseMdRaidUUID(c.in); got != c.want {
				t.Fatalf("parseMdRaidUUID() = %q, want %q", got, c.want)
			}
		})
	}
}
