package x2xlib

import (
	"encoding/json"

	"github.com/kisun-bit/drpkg/define"
	"github.com/pkg/errors"
)

// 签名
type Signature struct {
	Signer define.Signer
	Hash   define.Hash
}

// 驱动签名
type DriverSignature struct {
	OS         string      `json:"os"`
	Signatures []Signature `json:"signatures"`
}

var windowsSigners = map[define.Signer]struct{}{
	define.DrvSignerPrivate:   {},
	define.DrvSignerVendor:    {},
	define.DrvSignerMicrosoft: {},
	define.DrvSignerWHQL:      {},
}

var linuxSigners = map[define.Signer]struct{}{
	define.DrvSignerPrivate: {},
	define.DrvSignerVendor:  {},
	define.DrvSignerDistro:  {},
}

var windowsHashes = map[define.Hash]struct{}{
	define.DrvHashSHA1:   {},
	define.DrvHashSHA256: {},
}

var linuxHashes = map[define.Hash]struct{}{
	define.DrvHashUnknown: {},
	define.DrvHashSHA1:    {},
	define.DrvHashSHA224:  {},
	define.DrvHashSHA256:  {},
	define.DrvHashSHA384:  {},
	define.DrvHashSHA512:  {},
}

func LoadDriverSignature(str string) (*DriverSignature, error) {
	ds := &DriverSignature{}
	if err := json.Unmarshal([]byte(str), ds); err != nil {
		return nil, err
	}
	return ds, nil
}

func NewDriverSignature(osType string, signs []Signature) (*DriverSignature, error) {
	sign := DriverSignature{OS: osType}
	for _, s := range signs {
		if s.Signer == "" && s.Hash == "" {
			continue
		}
		sign.Signatures = append(sign.Signatures, s)
	}

	if err := sign.Check(); err != nil {
		return nil, err
	}
	return &sign, nil
}

func (ds *DriverSignature) String() string {
	data, _ := json.Marshal(ds)
	return string(data)
}

// Check 检查
func (ds *DriverSignature) Check() error {

	var (
		maxSignatures int
		signers       map[define.Signer]struct{}
		hashes        map[define.Hash]struct{}
	)

	switch ds.OS {
	case define.OsWindows:
		maxSignatures = 2
		signers = windowsSigners
		hashes = windowsHashes

	case define.OsLinux:
		maxSignatures = 1
		signers = linuxSigners
		hashes = linuxHashes

	default:
		return errors.Errorf("unsupported os %s", ds.OS)
	}

	if len(ds.Signatures) > maxSignatures {
		return errors.New("too many signatures")
	}

	for _, sig := range ds.Signatures {

		if _, ok := signers[sig.Signer]; !ok {
			return errors.Errorf(
				"unsupported signer %q for os %s",
				sig.Signer,
				ds.OS,
			)
		}

		if _, ok := hashes[sig.Hash]; !ok {
			return errors.Errorf(
				"unsupported hash %q for os %s",
				sig.Hash,
				ds.OS,
			)
		}
	}

	return nil
}

func (ds *DriverSignature) IsSha1() bool {
	return len(ds.Signatures) > 0 && ds.Signatures[0].Hash == define.DrvHashSHA1
}

// Weight 优先级权重
func (ds *DriverSignature) Weight() int {
	score := 0

	for _, sig := range ds.Signatures {
		score += signerWeight(ds.OS, sig.Signer)
		score += hashWeight(ds.OS, sig.Hash)
	}

	return score
}

func signerWeight(os string, signer define.Signer) int {

	switch os {

	case define.OsWindows:
		switch signer {
		case define.DrvSignerWHQL:
			return 4000
		case define.DrvSignerMicrosoft:
			return 3000
		case define.DrvSignerVendor:
			return 2000
		case define.DrvSignerPrivate:
			return 1000
		}

	case define.OsLinux:
		switch signer {
		case define.DrvSignerDistro:
			return 3000
		case define.DrvSignerVendor:
			return 2000
		case define.DrvSignerPrivate:
			return 1000
		}
	}

	return 0
}

func hashWeight(os string, hash define.Hash) int {

	_ = os

	switch hash {

	case define.DrvHashSHA512:
		return 60

	case define.DrvHashSHA384:
		return 50

	case define.DrvHashSHA256:
		return 40

	case define.DrvHashSHA224:
		return 30

	case define.DrvHashSHA1:
		return 20

	case define.DrvHashUnknown:
		return 10
	}

	return 0
}
