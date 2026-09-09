package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
)

// NewHTTPClient creates a TLS 1.3 client with an optional additional CA.
func NewHTTPClient(
	argServerURL string,
	argCACertificatePath string,
) (*http.Client, error) {
	if argCACertificatePath == "" {
		return http.DefaultClient, nil
	}

	parsedURL, err := url.Parse(argServerURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	if parsedURL.Scheme != "https" {
		return nil, errors.New(
			"a custom CA certificate requires an HTTPS server URL",
		)
	}

	certificatePEM, err := os.ReadFile(argCACertificatePath)
	if err != nil {
		return nil, fmt.Errorf("read CA certificate: %w", err)
	}
	rootCAs, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system certificate authorities: %w", err)
	}
	if !rootCAs.AppendCertsFromPEM(certificatePEM) {
		return nil, errors.New("CA certificate file contains no valid certificates")
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    rootCAs,
	}

	return &http.Client{Transport: transport}, nil
}
