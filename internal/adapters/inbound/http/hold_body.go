package http

import (
	"bytes"
	"io"
	"time"

	"github.com/IQVO/payment-provider-simulator/internal/application/views"
)

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

type movementBody struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	AmountMinor int64     `json:"amountMinor"`
	At          time.Time `json:"at"`
}

type holdDetail struct {
	ID             string         `json:"id"`
	Status         string         `json:"status"`
	Scenario       string         `json:"scenario"`
	Reference      string         `json:"reference"`
	Currency       string         `json:"currency"`
	HeldMinor      int64          `json:"heldMinor"`
	CapturedMinor  int64          `json:"capturedMinor"`
	RefundedMinor  int64          `json:"refundedMinor"`
	RemainingMinor int64          `json:"remainingMinor"`
	ExpiresAt      time.Time      `json:"expiresAt"`
	CreatedAt      time.Time      `json:"createdAt"`
	SupersededBy   string         `json:"supersededBy,omitempty"`
	Movements      []movementBody `json:"movements"`
}

func holdBody(h views.Hold) holdDetail {
	d := holdDetail{
		ID: h.ID, Status: h.Status, Scenario: h.Scenario, Reference: h.Reference, Currency: h.Currency,
		HeldMinor: h.HeldMinor, CapturedMinor: h.CapturedMinor, RefundedMinor: h.RefundedMinor,
		RemainingMinor: h.RemainingMinor, ExpiresAt: h.ExpiresAt, CreatedAt: h.CreatedAt,
		SupersededBy: h.SupersededBy, Movements: []movementBody{},
	}
	for _, m := range h.Movements {
		d.Movements = append(d.Movements, movementBody{ID: m.ID, Kind: m.Kind, AmountMinor: m.AmountMinor, At: m.At})
	}
	return d
}
