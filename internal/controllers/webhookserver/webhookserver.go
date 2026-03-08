package webhookserver

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/nats-io/nats.go"
	"github.com/open-uem/openuem-console/internal/controllers/webhookserver/handlers"
	"github.com/open-uem/openuem-console/internal/models"
)

type WebhookServer struct {
	Router  *echo.Echo
	Handler *handlers.Handler
	Server  *http.Server
}

func New(m *models.Model, natsConn *nats.Conn) *WebhookServer {
	w := WebhookServer{}

	w.Router = echo.New()
	w.Router.HideBanner = true
	w.Router.HidePort = true

	w.Handler = handlers.NewHandler(m, natsConn)
	w.Handler.Register(w.Router)

	return &w
}

func (w *WebhookServer) Serve(address, certFile, certKey string) error {
	w.Server = &http.Server{
		Addr:    address,
		Handler: w.Router,
	}

	return w.Server.ListenAndServeTLS(certFile, certKey)
}

func (w *WebhookServer) Close() error {
	if w.Server != nil {
		return w.Server.Close()
	}
	return nil
}
