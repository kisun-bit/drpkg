package tempfile

import (
	"io/ioutil"
	"os"
	"sync"
)

var (
	mu sync.Mutex

	g_tempdir = os.TempDir()
)

func GetTempDir() string {
	mu.Lock()
	defer mu.Unlock()

	return g_tempdir
}

// Wrap ioutil.TempFile to ensure that temp files are always written
// in the correct directory.
func TempFile(pattern string) (f *os.File, err error) {
	// Force the temporary file to be placed in the global tempdir
	return ioutil.TempFile(GetTempDir(), pattern)
}

// Wrap ioutil.TempDir to ensure that temp files are always written
// in the correct directory.
func TempDir(pattern string) (string, error) {
	// Force the temporary file to be placed in the global tempdir
	return os.MkdirTemp(GetTempDir(), pattern)
}

func CreateTemp(pattern string) (f *os.File, err error) {
	return os.CreateTemp(GetTempDir(), pattern)
}

func SetTempDir(path string) error {
	mu.Lock()
	defer mu.Unlock()

	// Try to create a file in the directory to make sure we have
	// permissions and the directory exists.
	tmpfile, err := ioutil.TempFile(path, "tmp")
	if err != nil {
		return err
	}
	tmpfile.Close()

	defer os.Remove(tmpfile.Name())

	g_tempdir = path

	return nil
}
