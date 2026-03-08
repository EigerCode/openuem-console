package handlers

import (
	"github.com/nats-io/nats.go"
	"github.com/open-uem/openuem-console/internal/models"
)

type Handler struct {
	Model          *models.Model
	NATSConnection *nats.Conn
}

func NewHandler(model *models.Model, natsConn *nats.Conn) *Handler {
	return &Handler{
		Model:          model,
		NATSConnection: natsConn,
	}
}
