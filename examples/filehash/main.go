package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/kisun-bit/drpkg/extend"
)

func main() {
	file := os.Args[1]

	f, e := os.Open(file)
	if e != nil {
		log.Fatalln("Open: ", e)
	}
	defer f.Close()

	hashFromFile, err := extend.FileMd5sum(f)
	if err != nil {
		log.Fatalln("FileMd5sum: ", err)
	}
	fmt.Println("hash from file: ", hashFromFile)

	hasher := md5.New()
	if _, err = extend.CopyFileByDiskExtents(file, hasher); err != nil {
		log.Fatalln("CopyFileByDiskExtents: ", err)
	}
	fmt.Println("hash from disk: ", hex.EncodeToString(hasher.Sum(nil)))
}
