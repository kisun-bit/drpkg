package x2xcore

import (
	"bufio"
	"os"
	"strings"

	"github.com/thoas/go-funk"
)

type INF struct {
	Sections map[string][]string
}

func ParseINF(path string) (*INF, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	inf := &INF{
		Sections: make(map[string][]string),
	}

	var sec string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		// 去掉行尾注释
		if i := strings.Index(line, ";"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			sec = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}

		if sec != "" {
			inf.Sections[sec] = append(inf.Sections[sec], line)
		}
	}

	return inf, scanner.Err()
}

func (inf *INF) ServiceNames() []string {
	var svcs []string

	for _, lines := range inf.Sections {
		for _, line := range lines {

			if !strings.HasPrefix(strings.ToLower(line), "addservice") {
				continue
			}

			i := strings.Index(line, "=")
			if i < 0 {
				continue
			}

			right := strings.TrimSpace(line[i+1:])

			fields := splitComma(right)
			if len(fields) == 0 {
				continue
			}

			svcs = append(svcs, fields[0])
		}
	}

	return funk.UniqString(svcs)
}

func splitComma(s string) []string {
	var out []string

	for _, v := range strings.Split(s, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}
