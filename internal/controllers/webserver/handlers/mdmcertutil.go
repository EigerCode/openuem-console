package handlers

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"time"

	"howett.net/plist"
)

const (
	appleWWDRCAURL = "https://www.apple.com/certificateauthority/AppleWWDRCAG3.cer"
	appleRootCAURL = "https://www.apple.com/appleca/AppleIncRootCertificate.cer"
)

// PushCertificateRequest is the structure Apple's identity.apple.com expects.
type PushCertificateRequest struct {
	PushCertRequestCSR       string `plist:"PushCertRequestCSR"`
	PushCertCertificateChain string `plist:"PushCertCertificateChain"`
	PushCertSignature        string `plist:"PushCertSignature"`
}

// BuildPushCertificateRequest assembles the full PushCertificateRequest.plist
// for upload to identity.apple.com.
// vendorKeyPEM: PEM-encoded vendor private key (from OpenUEM)
// vendorCertPEM: PEM-encoded vendor certificate (from OpenUEM)
// pushCSRPEM: PEM-encoded push CSR (generated per-tenant)
// Returns the base64-encoded plist data.
func BuildPushCertificateRequest(vendorKeyPEM, vendorCertPEM, pushCSRPEM string) ([]byte, error) {
	// Parse vendor private key
	keyBlock, _ := pem.Decode([]byte(vendorKeyPEM))
	if keyBlock == nil {
		return nil, fmt.Errorf("could not decode vendor private key PEM")
	}

	vendorKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		// Try PKCS8 format
		key, err2 := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("parse vendor private key: %w (PKCS8: %v)", err, err2)
		}
		var ok bool
		vendorKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("vendor private key is not RSA")
		}
	}

	// Parse vendor certificate and normalize to canonical PEM (matching mdmctl)
	certBlock, _ := pem.Decode([]byte(vendorCertPEM))
	if certBlock == nil {
		return nil, fmt.Errorf("could not decode vendor certificate PEM")
	}
	vendorCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse vendor certificate: %w", err)
	}
	// Re-encode from parsed cert to get canonical PEM (like mdmctl's pemCert)
	normalizedVendorPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: vendorCert.Raw})

	// Verify that vendor key matches vendor cert
	certPubKey, ok := vendorCert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("vendor certificate public key is not RSA")
	}
	if vendorKey.PublicKey.N.Cmp(certPubKey.N) != 0 || vendorKey.PublicKey.E != certPubKey.E {
		return nil, fmt.Errorf("vendor private key does not match vendor certificate public key")
	}

	// Parse push CSR to get DER bytes (fully parse like mdmctl)
	csrBlock, _ := pem.Decode([]byte(pushCSRPEM))
	if csrBlock == nil {
		return nil, fmt.Errorf("could not decode push CSR PEM")
	}
	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse push CSR: %w", err)
	}

	// Sign push CSR DER with vendor key (SHA256+RSA, matching mdmctl)
	h := sha256.New()
	h.Write(csr.Raw)
	signature, err := rsa.SignPKCS1v15(rand.Reader, vendorKey, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return nil, fmt.Errorf("sign push CSR: %w", err)
	}

	// Self-verify the signature before proceeding
	verifyHash := sha256.New()
	verifyHash.Write(csr.Raw)
	if err := rsa.VerifyPKCS1v15(certPubKey, crypto.SHA256, verifyHash.Sum(nil), signature); err != nil {
		return nil, fmt.Errorf("self-verification of signature failed: %w", err)
	}

	// Download Apple intermediate and root certs
	wwdrDER, err := downloadDERCert(appleWWDRCAURL)
	if err != nil {
		return nil, fmt.Errorf("download WWDR cert: %w", err)
	}
	rootDER, err := downloadDERCert(appleRootCAURL)
	if err != nil {
		return nil, fmt.Errorf("download Apple Root CA: %w", err)
	}

	// Build certificate chain: vendor cert + WWDR + Root CA (all PEM, matching mdmctl)
	chain := string(normalizedVendorPEM) + string(derToPEM(wwdrDER)) + string(derToPEM(rootDER))

	// Assemble the request
	req := &PushCertificateRequest{
		PushCertRequestCSR:       base64.StdEncoding.EncodeToString(csr.Raw),
		PushCertCertificateChain: chain,
		PushCertSignature:        base64.StdEncoding.EncodeToString(signature),
	}

	// Marshal to XML plist
	plistData, err := plist.MarshalIndent(req, plist.XMLFormat, "\t")
	if err != nil {
		return nil, fmt.Errorf("marshal plist: %w", err)
	}

	// Base64-encode the plist
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(plistData)))
	base64.StdEncoding.Encode(encoded, plistData)

	return encoded, nil
}

// downloadDERCert fetches a DER certificate from a URL.
func downloadDERCert(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Validate it parses as a certificate
	if _, err := x509.ParseCertificate(data); err != nil {
		return nil, fmt.Errorf("parse certificate from %s: %w", url, err)
	}

	return data, nil
}

// derToPEM converts DER certificate bytes to PEM.
func derToPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
