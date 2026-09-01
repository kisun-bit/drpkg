// Copyright 2018-present Network Optix, Inc. Licensed under MPL 2.0: www.mozilla.org/MPL/2.0/
package qcow2

import (
	"fmt"
	"math/rand"
	"path"
	"testing"
)

// benchImageSize is the virtual size used by the write benchmarks. Clusters
// are allocated lazily, so the host file stays small relative to this.
const benchImageSize = uint64(128 * 1024 * 1024)

// BenchmarkWriteSequential measures full image sequential write throughput
// for several block sizes, with and without the write back caches.
func BenchmarkWriteSequential(b *testing.B) {
	for _, blockSize := range []uint64{512, 64 * 1024, 1024 * 1024} {
		for _, useCache := range []bool{true, false} {
			cacheStr := "cache"
			if !useCache {
				cacheStr = "nocache"
			}
			b.Run(fmt.Sprintf("block=%d/%s", blockSize, cacheStr), func(b *testing.B) {
				imagePath := path.Join(testsDir(), "bench_seq.img")
				prepareTestDir(testsDir(), b)
				data := make([]byte, blockSize)
				for i := range data {
					data[i] = byte(i % 251)
				}
				b.SetBytes(int64(benchImageSize))
				for i := 0; i < b.N; i++ {
					b.StopTimer()
					deleteDiskIfExists(imagePath, b)
					image, err := NewImageFactory(useCache).CreateImage(imagePath, benchImageSize)
					if err != nil {
						b.Fatalf("error while creating an image: %s", err)
					}
					b.StartTimer()
					for offset := uint64(0); offset < benchImageSize; offset += blockSize {
						if err := image.WriteAt(offset, data); err != nil {
							b.Fatalf("error while writing at offset %d: %s", offset, err)
						}
					}
					if err := image.Close(); err != nil {
						b.Fatalf("error while closing the image: %s", err)
					}
					b.StopTimer()
				}
			})
		}
	}
}

// BenchmarkWriteRandom4K measures random 4K write throughput. Most writes hit
// previously unwritten clusters, which exercises cluster allocation.
func BenchmarkWriteRandom4K(b *testing.B) {
	const writesPerOp = 4096 // 16MB of 4K writes per iteration
	for _, useCache := range []bool{true, false} {
		cacheStr := "cache"
		if !useCache {
			cacheStr = "nocache"
		}
		b.Run(cacheStr, func(b *testing.B) {
			imagePath := path.Join(testsDir(), "bench_rand.img")
			prepareTestDir(testsDir(), b)
			clusterSize := uint64(64 * 1024)
			numberOfClusters := benchImageSize / clusterSize
			rng := rand.New(rand.NewSource(42))
			offsets := make([]uint64, writesPerOp)
			for index := range offsets {
				offsets[index] = uint64(rng.Intn(int(numberOfClusters)))*clusterSize +
					uint64(rng.Intn(int(clusterSize/4096)))*4096
			}
			data := make([]byte, 4096)
			for i := range data {
				data[i] = byte(i % 251)
			}
			b.SetBytes(int64(writesPerOp * 4096))
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				deleteDiskIfExists(imagePath, b)
				image, err := NewImageFactory(useCache).CreateImage(imagePath, benchImageSize)
				if err != nil {
					b.Fatalf("error while creating an image: %s", err)
				}
				b.StartTimer()
				for _, offset := range offsets {
					if err := image.WriteAt(offset, data); err != nil {
						b.Fatalf("error while writing at offset %d: %s", offset, err)
					}
				}
				if err := image.Close(); err != nil {
					b.Fatalf("error while closing the image: %s", err)
				}
				b.StopTimer()
			}
		})
	}
}
