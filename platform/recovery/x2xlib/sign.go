package x2xlib

import (
	"encoding/json"

	"github.com/kisun-bit/drpkg/defs"
	"github.com/pkg/errors"
)

// 签名
type Signature struct {
	Signer defs.Signer
	Hash   defs.Hash
}

// 驱动签名
type DriverSignature struct {
	OS         string      `json:"os"`
	Signatures []Signature `json:"signatures"`
}

var windowsSigners = map[defs.Signer]struct{}{
	defs.DrvSignerPrivate:   {},
	defs.DrvSignerVendor:    {},
	defs.DrvSignerMicrosoft: {},
	defs.DrvSignerWHQL:      {},
}

var linuxSigners = map[defs.Signer]struct{}{
	defs.DrvSignerPrivate: {},
	defs.DrvSignerVendor:  {},
	defs.DrvSignerDistro:  {},
}

var windowsHashes = map[defs.Hash]struct{}{
	defs.DrvHashSHA1:   {},
	defs.DrvHashSHA256: {},
}

var linuxHashes = map[defs.Hash]struct{}{
	defs.DrvHashUnknown: {},
	defs.DrvHashSHA1:    {},
	defs.DrvHashSHA224:  {},
	defs.DrvHashSHA256:  {},
	defs.DrvHashSHA384:  {},
	defs.DrvHashSHA512:  {},
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
		signers       map[defs.Signer]struct{}
		hashes        map[defs.Hash]struct{}
	)

	switch ds.OS {
	case defs.OsWindows:
		maxSignatures = 2
		signers = windowsSigners
		hashes = windowsHashes

	case defs.OsLinux:
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
	return len(ds.Signatures) > 0 && ds.Signatures[0].Hash == defs.DrvHashSHA1
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

func signerWeight(os string, signer defs.Signer) int {

	switch os {

	case defs.OsWindows:
		switch signer {
		case defs.DrvSignerWHQL:
			return 4000
		case defs.DrvSignerMicrosoft:
			return 3000
		case defs.DrvSignerVendor:
			return 2000
		case defs.DrvSignerPrivate:
			return 1000
		}

	case defs.OsLinux:
		switch signer {
		case defs.DrvSignerDistro:
			return 3000
		case defs.DrvSignerVendor:
			return 2000
		case defs.DrvSignerPrivate:
			return 1000
		}
	}

	return 0
}

func hashWeight(os string, hash defs.Hash) int {

	_ = os

	switch hash {

	case defs.DrvHashSHA512:
		return 60

	case defs.DrvHashSHA384:
		return 50

	case defs.DrvHashSHA256:
		return 40

	case defs.DrvHashSHA224:
		return 30

	case defs.DrvHashSHA1:
		return 20

	case defs.DrvHashUnknown:
		return 10
	}

	return 0
}
