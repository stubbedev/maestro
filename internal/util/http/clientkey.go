// Client certificates (ssl local_cert, local_pk, passphrase; curl's
// CURLOPT_SSLCERT, CURLOPT_SSLKEY, CURLOPT_SSLKEYPASSWD), loaded as
// OpenSSL loads PEM files, failing as libcurl 8.22 reports it.

package http

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/des" //nolint:gosec // PBE-SHA1-3DES and PBES2 DES keys: OpenSSL reads them, so curl does
	"crypto/pbkdf2"
	"crypto/sha1" //nolint:gosec // PKCS#12 KDF of PBE-SHA1-3DES and HMAC-SHA1 PBKDF2, as OpenSSL
	"crypto/sha256"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"hash"
	"io/fs"
	"os"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/scrypt"
)

// curleBadFunctionArgument is CURLE_BAD_FUNCTION_ARGUMENT, which libcurl
// 8.22 returns when the private key cannot be used.
const curleBadFunctionArgument = 43

// clientCertError is a client certificate curl cannot use.
type clientCertError struct {
	errno int
	msg   string
}

// loadClientCertificate reads local_cert (which may hold the key too) and
// local_pk, decrypting the key with passphrase: legacy encrypted PEM
// (Proc-Type: 4,ENCRYPTED) and encrypted PKCS#8 (PBES2 with PBKDF2 or
// scrypt and AES or 3DES/DES, or PKCS#12's
// pbeWithSHA1And3-KeyTripleDES-CBC); other PBES1 schemes are not
// supported. Failures read as curl's (lib/vtls/openssl.c, cert_stuff).
func loadClientCertificate(certFile, keyFile, passphrase string) (tls.Certificate, *clientCertError) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return tls.Certificate{}, certLoadError(certFile, opensslSysError(err))
	}

	if reason := checkPEMCertificate(certPEM); reason != "" {
		return tls.Certificate{}, certLoadError(certFile, reason)
	}

	if keyFile == "" {
		keyFile = certFile
	}

	keyErr := &clientCertError{curleBadFunctionArgument, "unable to set private key file: '" + keyFile + "' type PEM"}

	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return tls.Certificate{}, keyErr
	}

	keyBlock, ok := decryptPrivateKey(keyPEM, passphrase)
	if !ok {
		return tls.Certificate{}, keyErr
	}

	cert, err := tls.X509KeyPair(certPEM, pem.EncodeToMemory(keyBlock))
	if err != nil {
		if strings.Contains(err.Error(), "does not match") {
			return tls.Certificate{}, &clientCertError{curleSSLCertproblem, "Private key does not match the certificate public key"}
		}

		return tls.Certificate{}, keyErr
	}

	return cert, nil
}

func certLoadError(certFile, reason string) *clientCertError {
	return &clientCertError{curleSSLCertproblem, "could not load PEM client certificate from " + certFile + ", OpenSSL error " + reason + ", (no key found, wrong passphrase, or wrong file format?)"}
}

// opensslSysError is OpenSSL 3's error string for a failed fopen().
func opensslSysError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "error:80000002:system library::No such file or directory"
	case errors.Is(err, fs.ErrPermission):
		return "error:8000000D:system library::Permission denied"
	}

	return "error:80000015:system library::Is a directory"
}

// checkPEMCertificate is PEM_read_bio_X509 on the file: "" when its first
// certificate parses, else OpenSSL's error.
func checkPEMCertificate(data []byte) string {
	for {
		var block *pem.Block

		block, data = pem.Decode(data)
		if block == nil {
			return "error:0480006C:PEM routines::no start line"
		}

		if block.Type == "CERTIFICATE" || block.Type == "TRUSTED CERTIFICATE" || block.Type == "X509 CERTIFICATE" {
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return "error:0688010A:asn1 encoding routines::nested asn1 error"
			}

			return ""
		}
	}
}

// decryptPrivateKey is PEM_read_bio_PrivateKey with the passphrase: the
// first private key of the file, decrypted.
func decryptPrivateKey(data []byte, passphrase string) (*pem.Block, bool) {
	for {
		var block *pem.Block

		block, data = pem.Decode(data)
		if block == nil {
			return nil, false
		}

		switch {
		case block.Type == "ENCRYPTED PRIVATE KEY":
			der, err := decryptPKCS8(block.Bytes, []byte(passphrase))
			if err != nil {
				return nil, false
			}

			return &pem.Block{Type: "PRIVATE KEY", Bytes: der}, true
		case strings.HasSuffix(block.Type, "PRIVATE KEY"):
			//nolint:staticcheck // legacy PEM encryption is what OpenSSL-produced keys use
			if x509.IsEncryptedPEMBlock(block) {
				//nolint:staticcheck // see above
				der, err := x509.DecryptPEMBlock(block, []byte(passphrase))
				if err != nil {
					return nil, false
				}

				return &pem.Block{Type: block.Type, Bytes: der}, true
			}

			return block, true
		}
	}
}

var (
	oidPBES2            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 13}
	oidPBKDF2           = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 5, 12}
	oidScrypt           = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11591, 4, 11}
	oidPBEWithSHA13DES  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 12, 1, 3}
	oidHMACWithSHA1     = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 7}
	oidHMACWithSHA224   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 8}
	oidHMACWithSHA256   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 9}
	oidHMACWithSHA384   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 10}
	oidHMACWithSHA512   = asn1.ObjectIdentifier{1, 2, 840, 113549, 2, 11}
	oidAES128CBC        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 2}
	oidAES192CBC        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 22}
	oidAES256CBC        = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 1, 42}
	oidDESEDE3CBC       = asn1.ObjectIdentifier{1, 2, 840, 113549, 3, 7}
	oidDESCBC           = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 7}
	errUnsupportedPKCS8 = errors.New("unsupported PKCS#8 encryption")
)

// encryptedPrivateKeyInfo is PKCS#8's EncryptedPrivateKeyInfo.
type encryptedPrivateKeyInfo struct {
	Algorithm     pkix.AlgorithmIdentifier
	EncryptedData []byte
}

type pbes2Params struct {
	KeyDerivationFunc pkix.AlgorithmIdentifier
	EncryptionScheme  pkix.AlgorithmIdentifier
}

type pbkdf2Params struct {
	Salt           []byte
	IterationCount int
	KeyLength      int                      `asn1:"optional"`
	PRF            pkix.AlgorithmIdentifier `asn1:"optional"`
}

// scryptParams is RFC 7914's scrypt-params.
type scryptParams struct {
	Salt      []byte
	N         int
	R         int
	P         int
	KeyLength int `asn1:"optional"`
}

type pbeParams struct {
	Salt           []byte
	IterationCount int
}

// decryptPKCS8 decrypts an EncryptedPrivateKeyInfo to its PrivateKeyInfo.
func decryptPKCS8(der, password []byte) ([]byte, error) {
	var info encryptedPrivateKeyInfo
	if _, err := asn1.Unmarshal(der, &info); err != nil {
		return nil, err
	}

	var (
		block cipher.Block
		iv    []byte
	)

	switch {
	case info.Algorithm.Algorithm.Equal(oidPBES2):
		var params pbes2Params
		if _, err := asn1.Unmarshal(info.Algorithm.Parameters.FullBytes, &params); err != nil {
			return nil, err
		}

		var (
			keyLen   int
			newBlock func([]byte) (cipher.Block, error)
		)

		switch alg := params.EncryptionScheme.Algorithm; {
		case alg.Equal(oidAES128CBC):
			keyLen, newBlock = 16, aes.NewCipher
		case alg.Equal(oidAES192CBC):
			keyLen, newBlock = 24, aes.NewCipher
		case alg.Equal(oidAES256CBC):
			keyLen, newBlock = 32, aes.NewCipher
		case alg.Equal(oidDESEDE3CBC):
			keyLen, newBlock = 24, des.NewTripleDESCipher
		case alg.Equal(oidDESCBC):
			keyLen, newBlock = 8, des.NewCipher
		default:
			return nil, errUnsupportedPKCS8
		}

		if _, err := asn1.Unmarshal(params.EncryptionScheme.Parameters.FullBytes, &iv); err != nil {
			return nil, err
		}

		key, err := pbes2Key(params.KeyDerivationFunc, password, keyLen)
		if err != nil {
			return nil, err
		}

		if block, err = newBlock(key); err != nil {
			return nil, err
		}
	case info.Algorithm.Algorithm.Equal(oidPBEWithSHA13DES):
		var params pbeParams
		if _, err := asn1.Unmarshal(info.Algorithm.Parameters.FullBytes, &params); err != nil {
			return nil, err
		}

		pass := bmpString(password)
		key := pkcs12KDF(sha1.New, 1, pass, params.Salt, params.IterationCount, 24)
		iv = pkcs12KDF(sha1.New, 2, pass, params.Salt, params.IterationCount, 8)

		var err error
		if block, err = des.NewTripleDESCipher(key); err != nil { //nolint:gosec // the key format, not a choice
			return nil, err
		}
	default:
		return nil, errUnsupportedPKCS8
	}

	data := info.EncryptedData
	if len(iv) != block.BlockSize() || len(data) == 0 || len(data)%block.BlockSize() != 0 {
		return nil, errUnsupportedPKCS8
	}

	out := make([]byte, len(data))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, data)

	// PKCS#7 padding; a wrong password leaves garbage here (or in the
	// key, which the key pair check rejects)
	pad := int(out[len(out)-1])
	if pad == 0 || pad > block.BlockSize() {
		return nil, errUnsupportedPKCS8
	}

	for _, b := range out[len(out)-pad:] {
		if int(b) != pad {
			return nil, errUnsupportedPKCS8
		}
	}

	return out[:len(out)-pad], nil
}

// pbes2Key derives a PBES2 key: PBKDF2 (PKCS5_v2_PBKDF2_keyivgen) or
// scrypt (PKCS5_v2_scrypt_keyivgen).
func pbes2Key(kdfAlg pkix.AlgorithmIdentifier, password []byte, keyLen int) ([]byte, error) {
	switch {
	case kdfAlg.Algorithm.Equal(oidPBKDF2):
		var kdf pbkdf2Params
		if _, err := asn1.Unmarshal(kdfAlg.Parameters.FullBytes, &kdf); err != nil {
			return nil, err
		}

		prf := sha1.New

		switch alg := kdf.PRF.Algorithm; {
		case len(alg) == 0, alg.Equal(oidHMACWithSHA1):
		case alg.Equal(oidHMACWithSHA224):
			prf = sha256.New224
		case alg.Equal(oidHMACWithSHA256):
			prf = sha256.New
		case alg.Equal(oidHMACWithSHA384):
			prf = sha512.New384
		case alg.Equal(oidHMACWithSHA512):
			prf = sha512.New
		default:
			return nil, errUnsupportedPKCS8
		}

		return pbkdf2.Key(prf, string(password), kdf.Salt, kdf.IterationCount, keyLen)
	case kdfAlg.Algorithm.Equal(oidScrypt):
		var kdf scryptParams
		if _, err := asn1.Unmarshal(kdfAlg.Parameters.FullBytes, &kdf); err != nil {
			return nil, err
		}

		// a keyLength must be the cipher's (PKCS5_v2_scrypt_keyivgen)
		if kdf.KeyLength != 0 && kdf.KeyLength != keyLen {
			return nil, errUnsupportedPKCS8
		}

		if !scryptParamsOK(kdf.N, kdf.R, kdf.P) {
			return nil, errUnsupportedPKCS8
		}

		return scrypt.Key(password, kdf.Salt, kdf.N, kdf.R, kdf.P, keyLen)
	}

	return nil, errUnsupportedPKCS8
}

// scryptMaxMem is the memory OpenSSL lets scrypt use when decrypting a
// key: EVP_PBE_scrypt_ex's default, SCRYPT_MAX_MEM (32 MiB).
const scryptMaxMem = 32 << 20

// scryptParamsOK is OpenSSL's check of scrypt parameters (providers/
// implementations/kdfs/scrypt.c, scrypt_alg): N a power of two from 2, r
// and p positive, p*r below 2^30, N below 2^(16r), and the buffers
// (128*r*p bytes for B, 128*r*(N+2) for V) within scryptMaxMem; keys
// beyond it fail to load ("memory limit exceeded").
func scryptParamsOK(n, r, p int) bool {
	if r <= 0 || p <= 0 || n < 2 || n&(n-1) != 0 {
		return false
	}

	if p > (1<<30-1)/r {
		return false
	}

	if 16*r <= 63 && uint64(n) >= uint64(1)<<(16*r) {
		return false
	}

	blen := uint64(p) * 128 * uint64(r)
	vlen := 128 * uint64(r) * (uint64(n) + 2)

	return blen+vlen <= scryptMaxMem
}

// bmpString is a password as PKCS#12 feeds it to its KDF: UTF-16BE with a
// terminating NUL.
func bmpString(password []byte) []byte {
	units := utf16.Encode([]rune(string(password)))
	out := make([]byte, 0, 2*len(units)+2)

	for _, u := range units {
		out = append(out, byte(u>>8), byte(u)) //nolint:gosec // the two bytes of a UTF-16 unit
	}

	return append(out, 0, 0)
}

// pkcs12KDF is the key derivation of RFC 7292, appendix B.2: id 1 makes a
// key, 2 an IV.
func pkcs12KDF(newHash func() hash.Hash, id byte, password, salt []byte, iterations, size int) []byte {
	h := newHash()
	v := h.BlockSize()

	fill := func(src []byte) []byte {
		if len(src) == 0 {
			return nil
		}

		n := v * ((len(src) + v - 1) / v)
		out := make([]byte, n)

		for i := range out {
			out[i] = src[i%len(src)]
		}

		return out
	}

	d := make([]byte, v)
	for i := range d {
		d[i] = id
	}

	i := append(fill(salt), fill(password)...)
	out := make([]byte, 0, size)

	for len(out) < size {
		h.Reset()
		h.Write(d)
		h.Write(i)
		a := h.Sum(nil)

		for range iterations - 1 {
			h.Reset()
			h.Write(a)
			a = h.Sum(a[:0])
		}

		out = append(out, a...)

		// I_j = (I_j + B + 1) mod 2^v for each v-byte block of I
		b := fill(a)[:v]

		for j := 0; j < len(i); j += v {
			carry := 1

			for k := v - 1; k >= 0; k-- {
				sum := int(i[j+k]) + int(b[k]) + carry
				i[j+k] = byte(sum) //nolint:gosec // the low byte; the carry keeps the rest
				carry = sum >> 8
			}
		}
	}

	return out[:size]
}
