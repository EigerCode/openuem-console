package reposerver

import (
	"crypto/tls"
	"crypto/x509"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/open-uem/openuem-console/internal/controllers/reposerver/handlers"
	"github.com/open-uem/openuem-console/internal/models"
	"github.com/open-uem/utils"
)

// RepoServer serves Munki/CIMIAN manifests and catalogs over mTLS.
type RepoServer struct {
	Router           *echo.Echo
	Handler          *handlers.Handler
	Server           *http.Server
	CACert           *x509.Certificate
	RepoClientCACert *x509.Certificate
}

// New creates a new RepoServer instance.
// caCertPath is used for handler logic; repoCACertPath is used for mTLS client validation.
func New(m *models.Model, caCertPath string, repoCACertPath string) *RepoServer {
	r := RepoServer{}

	var err error
	r.CACert, err = utils.ReadPEMCertificate(caCertPath)
	if err != nil {
		log.Fatalf("[FATAL]: could not read CA certificate for repo server: %v", err)
	}

	if repoCACertPath != "" && repoCACertPath != caCertPath {
		r.RepoClientCACert, err = utils.ReadPEMCertificate(repoCACertPath)
		if err != nil {
			log.Fatalf("[FATAL]: could not read repo CA certificate: %v", err)
		}
	}

	// Minimal Echo router — no sessions, no CSRF, no i18n needed
	r.Router = echo.New()
	r.Router.HideBanner = true
	r.Router.Use(middleware.Recover())
	r.Router.Use(middleware.Logger())

	// Create handler and register routes
	r.Handler = handlers.NewHandler(m, r.CACert)
	r.Handler.Register(r.Router)

	return &r
}

// Serve starts the repo server with mandatory mTLS client certificate verification.
func (r *RepoServer) Serve(address, certFile, certKey string) error {
	cp := x509.NewCertPool()
	if r.RepoClientCACert != nil {
		cp.AddCert(r.RepoClientCACert)
	} else {
		cp.AddCert(r.CACert)
	}

	r.Server = &http.Server{
		Addr:    address,
		Handler: r.Router,
		TLSConfig: &tls.Config{
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  cp,
		},
	}

	return r.Server.ListenAndServeTLS(certFile, certKey)
}

// Close gracefully shuts down the repo server.
func (r *RepoServer) Close() error {
	if r.Server != nil {
		return r.Server.Close()
	}
	return nil
}
