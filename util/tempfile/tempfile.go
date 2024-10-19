package tempfile

import (
	"fmt"
	"github.com/kisun-bit/drpkg/util"
	"runtime"
	"sync"
	"time"
)

var (
	gTracker *TmpFileTracker
)

type TmpFile struct {
	name               string
	callsite_added     string
	callsite_removed   string
	age                time.Time
	created, destroyed time.Time
	err                error
}

type TmpFileTracker struct {
	mu    sync.Mutex
	files map[string]*TmpFile
}

func (self *TmpFileTracker) scan() {
	// Keep some recent files around for a max of 1 min, but always
	// keep tmpfiles that are in use.
	if len(self.files) > 20 {
		now := util.Now()
		cutoff := now.Add(-time.Minute)

		var expired []string
		for k, v := range self.files {
			if !v.destroyed.IsZero() &&
				v.age.Before(cutoff) {
				expired = append(expired, k)
			}
		}

		for _, k := range expired {
			delete(self.files, k)
		}
	}

	// Something went wrong! avoid memory leaks.
	if len(self.files) > 1000 {
		self.files = make(map[string]*TmpFile)
	}
}

func (self *TmpFileTracker) AddTmpFile(filename string) {
	self.mu.Lock()
	defer self.mu.Unlock()

	callsite := ""
	_, file, no, ok := runtime.Caller(2)
	if ok {
		callsite = fmt.Sprintf("%s#%d", file, no)
	}

	self.files[filename] = &TmpFile{
		name:           filename,
		created:        util.Now(),
		callsite_added: callsite,
	}
	self.scan()
}

func (self *TmpFileTracker) RemoveTmpFile(filename string, err error) {
	self.mu.Lock()
	defer self.mu.Unlock()

	callsite := ""
	_, file, no, ok := runtime.Caller(2)
	if ok {
		callsite = fmt.Sprintf("%s#%d", file, no)
	}

	record, pres := self.files[filename]
	if !pres {
		record = &TmpFile{
			name: filename,
		}
	}

	now := util.Now()
	record.destroyed = now
	record.age = now
	record.err = err
	record.callsite_removed = callsite

	self.files[filename] = record

	self.scan()
}

func AddTmpFile(filename string) {
	gTracker.AddTmpFile(filename)
}

func RemoveTmpFile(filename string, err error) {
	gTracker.RemoveTmpFile(filename, err)
}
